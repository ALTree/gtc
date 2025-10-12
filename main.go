package main

import (
	"fmt"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/ALTree/perfetto"
	"golang.org/x/exp/trace"
)

type Stats struct {
	Total   int
	Skipped int
}

func main() {

	file, err := os.Open(os.Args[1])
	if err != nil {
		panic(err)
	}

	ext := path.Ext(os.Args[1])
	outfile, err := os.Create(strings.Replace(os.Args[1], ext, ".proto", 1))
	if err != nil {
		panic(err)
	}

	var stats Stats
	tr, _ := trace.NewReader(file)

	pt := perfetto.NewTrace()
	pt.AddProcess(0, "Process")
	glb := perfetto.GlobalTrack()

	running := make(map[int64]bool)
	stacks := make(map[int64][]trace.StackFrame)
	activeRanges := make(map[int64]string)

	var e trace.Event
	for err == nil {
		stats.Total++
		if stats.Total%10000 == 0 {
			WriteFile(&pt, outfile)
			pt.Reset()
		}

		e, err = tr.ReadEvent()
		ts := uint64(e.Time())

		t := int32(e.Thread())
		if _, ok := pt.Threads[t]; !ok {
			pt.AddThread(0, t, "Thread")
		}

		switch k := e.Kind(); k {
		case trace.EventMetric:
			name := e.Metric().Name
			if _, ok := pt.Counters[name]; !ok {
				pt.AddCounter(name, "")
			}
			pt.NewValue(pt.Counters[name], ts, int64(e.Metric().Value.Uint64()))
		case trace.EventRangeBegin, trace.EventRangeEnd:
			r := e.Range()
			if k == trace.EventRangeBegin {
				if r.Scope.Kind == trace.ResourceNone {
					pt.StartSlice(glb, ts, r.Name)
				} else {
					if s := e.Range().Scope; s.Kind == trace.ResourceGoroutine {
						gID := int64(e.Range().Scope.Goroutine())
						activeRanges[gID] = r.Name
					}
					pt.StartSlice(pt.Threads[t], ts, r.Name)
				}
			} else {
				if r.Scope.Kind == trace.ResourceNone {
					pt.EndSlice(glb, ts)
				} else {
					if s := e.Range().Scope; s.Kind == trace.ResourceGoroutine {
						gID := int64(e.Range().Scope.Goroutine())
						activeRanges[gID] = ""
					}
					pt.EndSlice(pt.Threads[t], ts)
				}
			}
		case trace.EventStateTransition:
			var gID int64
			k := e.StateTransition().Resource.Kind
			if k == trace.ResourceGoroutine {
				gID = int64(e.StateTransition().Resource.Goroutine())
			} else {
				continue
			}

			from, to := e.StateTransition().Goroutine()

			// if we're coming from the Syscall state, close syscall slice
			if from == trace.GoSyscall {
				pt.EndSlice(pt.Threads[t], ts)
			}

			// if we're going to the Syscall state, open a syscall slice
			if to == trace.GoSyscall {
				stack := slices.Collect(e.Stack().Frames())
				pt.StartSlice(pt.Threads[t], ts, "syscall", StackToAnnotations(stack))
				continue
			}

			// If we're going to Runnable, collect the starting stack
			// so we can use the Func in the last Frame to display the
			// goroutine starting function when, later, it goes to
			// Running and we'll start a slice for it.
			if to == trace.GoRunnable {
				stack := slices.Collect(e.StateTransition().Stack.Frames())
				if len(stack) > 0 {
					stacks[gID] = stack
				} else { // try to collect stack from the Event
					stack := slices.Collect(e.Stack().Frames())
					if len(stack) > 0 {
						stacks[gID] = stack
					}
				}
			}

			// If we're going to Running, start a running goroutine
			// slice. Otherwise, close it.
			if to == trace.GoRunning {
				if _, ok := running[gID]; !ok {
					var gfunc string
					if stack, ok := stacks[gID]; ok {
						if s := stack[len(stack)-1].Func; s != "" {
							gfunc = " (" + s + ")"
						}
					}

					//pt.AddEvent(pt.Threads[t].StartSlice(ts, fmt.Sprintf("G%v%v", gID, gfunc), StackToAnnotations(stacks[gID])))
					pt.StartSlice(pt.Threads[t], ts, fmt.Sprintf("G%v%v", gID, gfunc))
					if ar, ok := activeRanges[gID]; ok && ar != "" {
						pt.StartSlice(pt.Threads[t], ts, ar)
					}
					running[gID] = true
				}
			} else {
				if _, ok := running[gID]; ok {
					if ar, ok := activeRanges[gID]; ok && ar != "" {
						pt.EndSlice(pt.Threads[t], ts)
					}
					pt.EndSlice(pt.Threads[t], ts)
					delete(running, gID)
				}
			}
		default:
			stats.Skipped++
		}
	}

	WriteFile(&pt, outfile)
	outfile.Close()
	fmt.Printf("Written %v\n", outfile.Name())
	fmt.Printf("Events: \t%v\nProcessed:\t%v\nSkipped: \t%v\n",
		stats.Total, stats.Total-stats.Skipped, stats.Skipped)
}

func StackToAnnotations(arr []trace.StackFrame) perfetto.Annotations {
	var res perfetto.Annotations
	for i, v := range arr {
		res = append(res, perfetto.KV{
			strconv.Itoa(i),
			v.Func + ":" + strconv.Itoa(int(v.Line)),
		})
	}
	return res
}

func WriteFile(pt *perfetto.Trace, f *os.File) {
	data, err := pt.Marshal()
	if err != nil {
		fmt.Println(err)
		return
	}
	_, err = f.Write(data)
	if err != nil {
		fmt.Println(err)
		return
	}
}
