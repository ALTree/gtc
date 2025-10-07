package main

import (
	"fmt"
	"os"
	"slices"

	"github.com/ALTree/perfetto"
	"golang.org/x/exp/trace"
)

func main() {

	file, err := os.Open("trace2.out")
	if err != nil {
		panic(err)
	}
	tr, _ := trace.NewReader(file)

	ptrace := perfetto.Trace{TID: 42}
	p := ptrace.AddProcess(0, "Process")
	threads := make(map[trace.ThreadID]perfetto.Thread)
	metrics := make(map[string]perfetto.Counter)
	running := make(map[int64]bool)
	stacks := make(map[int64]string)

	var e trace.Event
	for err == nil {
		e, err = tr.ReadEvent()
		fmt.Println("|", e)
		switch e.Kind() {
		case trace.EventMetric:
			name := e.Metric().Name
			if _, ok := metrics[name]; !ok {
				metrics[name] = ptrace.AddCounter(name, "")
			}
			ptrace.AddEvent(metrics[name].NewValue(uint64(e.Time()), int64(e.Metric().Value.Uint64())))
		case trace.EventRangeBegin, trace.EventRangeEnd:
			r := e.Range()
			t := e.Thread()
			if _, ok := threads[t]; !ok {
				threads[t] = ptrace.AddThread(0, int32(t), "Thread")
			}
			if k := e.Kind(); k == trace.EventRangeBegin {
				if r.Scope.Kind == trace.ResourceNone {
					ptrace.AddEvent(p.StartSlice(uint64(e.Time()), r.Name))
				} else {
					ptrace.AddEvent(threads[t].StartSlice(uint64(e.Time()), r.Name))
				}
			} else {
				if r.Scope.Kind == trace.ResourceNone {
					ptrace.AddEvent(p.EndSlice(uint64(e.Time())))
				} else {
					ptrace.AddEvent(threads[t].EndSlice(uint64(e.Time())))
				}
			}
		case trace.EventStateTransition:
			if e.StateTransition().Resource.Kind != trace.ResourceGoroutine {
				continue
			}

			// first, extract the thread and add a new thread to the
			// trace if we never saw this one
			t := e.Thread()
			if _, ok := threads[t]; !ok {
				threads[t] = ptrace.AddThread(0, int32(t), "Thread")
			}

			gID := int64(e.StateTransition().Resource.Goroutine())
			from, to := e.StateTransition().Goroutine()

			// if we're coming from Syscall, close syscall slice
			if from == trace.GoSyscall {
				fmt.Printf("> Closed syscall for Goroutine %v\n", gID)
				ptrace.AddEvent(threads[t].EndSlice(uint64(e.Time())))
			}

			// if we're going to Syscall, open a syscall slice
			if to == trace.GoSyscall {
				fmt.Printf("> Goroutine %v in syscall \n", gID)
				ptrace.AddEvent(threads[t].StartSlice(uint64(e.Time()), "syscall"))
				// we continue because we opened the slice related to
				// the 'to' parameter, and the 'from' is irrelevant
				// (we're coming from running, but we don't need to
				// close the corresponding running slice when a
				// syscall starts because syscall is considered a
				// running state).
				continue
			}

			if to == trace.GoRunnable {
				stack := e.StateTransition().Stack.Frames()
				var sf trace.StackFrame
				if sc := slices.Collect(stack); len(sc) > 0 {
					sf = sc[len(sc)-1]
					stacks[gID] = sf.Func
				}
			}

			if to == trace.GoRunning {
				if _, ok := running[gID]; !ok {
					running[gID] = true
					fmt.Printf("> put gc %v to running, func: %v \n", gID, stacks[gID])
					ptrace.AddEvent(threads[t].StartSlice(uint64(e.Time()), fmt.Sprintf("G%v (%v)", gID, stacks[gID])))
				}
			} else {
				if _, ok := running[gID]; ok {
					fmt.Printf("> closed running slice for %v \n", gID)
					ptrace.AddEvent(threads[t].EndSlice(uint64(e.Time())))
					delete(running, gID)
					//delete(stacks, gID)
				}
			}
		}
	}

	data, err := ptrace.Marshal()
	if err != nil {
		fmt.Println(err)
		return
	}

	err = os.WriteFile("trace.proto", data, 0666)
	if err != nil {
		fmt.Println(err)
	}

}
