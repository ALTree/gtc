package main

import (
	"fmt"
	"os"

	"github.com/ALTree/perfetto"
	"golang.org/x/exp/trace"
)

func RangeString(e trace.Event) string {
	if k := e.Kind(); k == trace.EventRangeBegin || k == trace.EventRangeEnd {
		r := e.Range()
		ss := "[Range Start]"
		if k == trace.EventRangeEnd {
			ss = "[Range End]"
		}
		return fmt.Sprintf("%v %s %v: %v", e.Time(), ss, r.Scope, r.Name)
	}
	panic("unreachable")
}

func StateTransitionString(e trace.Event) string {
	if e.Kind() == trace.EventStateTransition {
		switch e.StateTransition().Resource.Kind {
		case trace.ResourceGoroutine:
			from, to := e.StateTransition().Goroutine()
			return fmt.Sprintf("%v [StateTransition] %v {%v}: %v -> %v",
				e.Time(), e.StateTransition().Resource, e.Thread(), from, to)
		case trace.ResourceProc:
			from, to := e.StateTransition().Proc()
			return fmt.Sprintf("%v [StateTransition] %v {%v}: %v -> %v",
				e.Time(), e.StateTransition().Resource, e.Thread(), from, to)
		}
	}
	panic("unreachable")
}

func main() {

	file, err := os.Open("trace.out")
	if err != nil {
		panic(err)
	}
	tr, _ := trace.NewReader(file)

	ptrace := perfetto.Trace{TID: 42}
	p := ptrace.AddProcess(0, "Process 0")
	globalT := ptrace.AddThread(0, 1, "Global Thread")

	running := make(map[int64]bool)

	var e trace.Event
	for err == nil {
		e, err = tr.ReadEvent()
		switch e.Kind() {
		// case trace.EventMetric:
		// 	fmt.Println("metric:", e.Metric())
		case trace.EventRangeBegin, trace.EventRangeEnd:
			fmt.Println(RangeString(e))
			r := e.Range()
			if k := e.Kind(); k == trace.EventRangeBegin {
				if r.Scope.Kind == trace.ResourceNone {
					ptrace.AddEvent(globalT.StartSlice(uint64(e.Time()), r.Name))
				} else {
					ptrace.AddEvent(p.StartSlice(uint64(e.Time()), r.Name))
				}
			} else {
				if r.Scope.Kind == trace.ResourceNone {
					ptrace.AddEvent(globalT.EndSlice(uint64(e.Time())))
				} else {
					ptrace.AddEvent(p.EndSlice(uint64(e.Time())))
				}
			}
		case trace.EventTaskBegin, trace.EventTaskEnd:
			//fmt.Println("task:", e.Task())
		case trace.EventRegionBegin, trace.EventRegionEnd:
			//fmt.Println("region:", e.Region())
		case trace.EventStateTransition:
			fmt.Println(StateTransitionString(e))
			if e.StateTransition().Resource.Kind == trace.ResourceGoroutine {
				_, to := e.StateTransition().Goroutine()
				gID := int64(e.StateTransition().Resource.Goroutine())
				if _, ok := running[gID]; !ok && to.Executing() {
					running[gID] = true
					fmt.Printf("> put gc %v to running \n", gID)
					ptrace.AddEvent(p.StartSlice(uint64(e.Time()), fmt.Sprintf("%v running", gID)))
				} else {
					if _, ok := running[gID]; ok {
						fmt.Printf("> closed running slice for %v \n", gID)
						ptrace.AddEvent(p.EndSlice(uint64(e.Time())))
						delete(running, gID)
					}
				}
			}
		case trace.EventLabel:
			fmt.Println("label:", e.Label())
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
