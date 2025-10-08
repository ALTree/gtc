package main

import (
	"flag"
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

	verbose := flag.Bool("v", false, "Run in verbose mode")
	startTS := flag.Uint64("s", 0, "Start timestamp for verbose prints")
	endTS := flag.Uint64("e", 1<<63, "End timestamp for verbose prints")
	flag.Parse()

	file, err := os.Open(flag.Args()[0])
	if err != nil {
		panic(err)
	}
	tr, _ := trace.NewReader(file)

	pt := perfetto.Trace{TID: 42}
	p := pt.AddProcess(0, "Process")
	running := make(map[int64]bool)
	stacks := make(map[int64]string)
	activeRanges := make(map[int64]string)

	var e trace.Event
	for err == nil {
		e, err = tr.ReadEvent()
		ts := uint64(e.Time())
		if *verbose {
			if ts >= *startTS && ts <= *endTS {
				fmt.Println("|", e)
			}
		}

		t := int32(e.Thread())
		if _, ok := pt.Threads[t]; !ok {
			pt.AddThread(0, t, "Tread")
		}

		switch k := e.Kind(); k {
		case trace.EventMetric:
			name := e.Metric().Name
			if _, ok := pt.Counters[name]; !ok {
				pt.AddCounter(name, "")
			}
			pt.AddEvent(pt.Counters[name].NewValue(ts, int64(e.Metric().Value.Uint64())))
		case trace.EventRangeBegin, trace.EventRangeEnd:
			r := e.Range()
			if k == trace.EventRangeBegin {
				if r.Scope.Kind == trace.ResourceNone {
					pt.AddEvent(p.StartSlice(ts, r.Name))
				} else {
					if s := e.Range().Scope; s.Kind == trace.ResourceGoroutine {
						gID := int64(e.Range().Scope.Goroutine())
						activeRanges[gID] = r.Name
					}
					pt.AddEvent(pt.Threads[t].StartSlice(ts, r.Name))
				}
			} else {
				if r.Scope.Kind == trace.ResourceNone {
					pt.AddEvent(p.EndSlice(ts))
				} else {
					if s := e.Range().Scope; s.Kind == trace.ResourceGoroutine {
						gID := int64(e.Range().Scope.Goroutine())
						activeRanges[gID] = ""
					}
					pt.AddEvent(pt.Threads[t].EndSlice(ts))
				}
			}
		case trace.EventStateTransition:
			if e.StateTransition().Resource.Kind != trace.ResourceGoroutine {
				continue
			}

			gID := int64(e.StateTransition().Resource.Goroutine())
			from, to := e.StateTransition().Goroutine()

			// if we're coming from the Syscall state, close syscall slice
			if from == trace.GoSyscall {
				pt.AddEvent(pt.Threads[t].EndSlice(ts))
			}

			// if we're going to the Syscall state, open a syscall slice
			if to == trace.GoSyscall {
				pt.AddEvent(pt.Threads[t].StartSlice(ts, "syscall"))
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
				if sc := slices.Collect(stack); len(sc) > 0 {
					sf := sc[len(sc)-1]
					stacks[gID] = sf.Func
				}
			}

			// If we're going to Running, start a running goroutine
			// slice. Otherwise, close it.
			if to == trace.GoRunning {
				if _, ok := running[gID]; !ok {
					pt.AddEvent(pt.Threads[t].StartSlice(ts, fmt.Sprintf("G%v (%v)", gID, stacks[gID])))
					if ar, ok := activeRanges[gID]; ok && ar != "" {
						pt.AddEvent(pt.Threads[t].StartSlice(ts, ar))
					}
					running[gID] = true
				}
			} else {
				if _, ok := running[gID]; ok {
					if ar, ok := activeRanges[gID]; ok && ar != "" {
						pt.AddEvent(pt.Threads[t].EndSlice(ts))
					}
					pt.AddEvent(pt.Threads[t].EndSlice(ts))
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
