package main

import (
	"fmt"
	"os"
	"slices"

	"github.com/ALTree/perfetto"
	"golang.org/x/exp/trace"
)

type Converter struct {
	Trace perfetto.Trace
}

func EmitMetric(tr perfetto.Trace, e trace.Event) {

}

func main() {
	file, err := os.Open(os.Args[1])
	if err != nil {
		panic(err)
	}
	tr, _ := trace.NewReader(file)

	pt := perfetto.Trace{TID: 42}
	p := pt.AddProcess(0, "Process")
	metrics := make(map[string]perfetto.Counter)
	running := make(map[int64]bool)
	stacks := make(map[int64]string)
	activeRanges := make(map[int64]string)

	var e trace.Event
	for err == nil {
		e, err = tr.ReadEvent()
		if len(os.Args) > 2 && os.Args[2] == "-v" {
			fmt.Println("|", e)
		}

		// Extract the thread, and add a new Thread to the trace if we
		// never saw this one
		t := int32(e.Thread())

		if _, ok := pt.Threads[t]; !ok {
			pt.AddThread(0, t, "Tread")
		}

		switch e.Kind() {
		case trace.EventMetric:
			name := e.Metric().Name
			if _, ok := metrics[name]; !ok {
				metrics[name] = pt.AddCounter(name, "")
			}
			pt.AddEvent(metrics[name].NewValue(uint64(e.Time()), int64(e.Metric().Value.Uint64())))
		case trace.EventRangeBegin, trace.EventRangeEnd:
			r := e.Range()
			if k := e.Kind(); k == trace.EventRangeBegin {
				if r.Scope.Kind == trace.ResourceNone {
					pt.AddEvent(p.StartSlice(uint64(e.Time()), r.Name))
				} else {
					if s := e.Range().Scope; s.Kind == trace.ResourceGoroutine {
						gID := int64(e.Range().Scope.Goroutine())
						activeRanges[gID] = r.Name
					}
					pt.AddEvent(pt.Threads[t].StartSlice(uint64(e.Time()), r.Name))
				}
			} else {
				if r.Scope.Kind == trace.ResourceNone {
					pt.AddEvent(p.EndSlice(uint64(e.Time())))
				} else {
					if s := e.Range().Scope; s.Kind == trace.ResourceGoroutine {
						gID := int64(e.Range().Scope.Goroutine())
						activeRanges[gID] = ""
					}
					pt.AddEvent(pt.Threads[t].EndSlice(uint64(e.Time())))
				}
			}
		case trace.EventStateTransition:
			if e.StateTransition().Resource.Kind != trace.ResourceGoroutine {
				continue
			}

			gID := int64(e.StateTransition().Resource.Goroutine())
			from, to := e.StateTransition().Goroutine()

			// if we're coming from Syscall, close syscall slice
			if from == trace.GoSyscall {
				pt.AddEvent(pt.Threads[t].EndSlice(uint64(e.Time())))
			}

			// if we're going to Syscall, open a syscall slice
			if to == trace.GoSyscall {
				pt.AddEvent(pt.Threads[t].StartSlice(uint64(e.Time()), "syscall"))
				// we continue because we opened the slice related to
				// the 'to' parameter, and the 'from' is irrelevant
				// (we're coming from running, but we don't need to
				// close the corresponding running slice when a
				// syscall starts because syscall is considered a
				// running state).
				continue
			}

			// If we're going to Runnable, collect the starting stack
			// so we can use the Func in the last Frame to display the
			// goroutine starting function when, later, it goes to
			// Running and we'll start a slice for it.
			if to == trace.GoRunnable {
				stack := e.StateTransition().Stack.Frames()
				var sf trace.StackFrame
				if sc := slices.Collect(stack); len(sc) > 0 {
					sf = sc[len(sc)-1]
					stacks[gID] = sf.Func
				}
			}

			// If we're going to Running, start a running goroutine
			// slice. Otherwise, close it.
			if to == trace.GoRunning {
				if _, ok := running[gID]; !ok {
					running[gID] = true
					pt.AddEvent(pt.Threads[t].StartSlice(uint64(e.Time()), fmt.Sprintf("G%v (%v)", gID, stacks[gID])))
					if ar, ok := activeRanges[gID]; ok && ar != "" {
						pt.AddEvent(pt.Threads[t].StartSlice(uint64(e.Time()), ar))
					}
				}
			} else {
				if _, ok := running[gID]; ok {
					if ar, ok := activeRanges[gID]; ok && ar != "" {
						pt.AddEvent(pt.Threads[t].EndSlice(uint64(e.Time())))
					}

					pt.AddEvent(pt.Threads[t].EndSlice(uint64(e.Time())))
					delete(running, gID)
				}
			}
		}
	}

	data, err := pt.Marshal()
	if err != nil {
		fmt.Println(err)
		return
	}

	err = os.WriteFile("trace.proto", data, 0666)
	if err != nil {
		fmt.Println(err)
	}

}
