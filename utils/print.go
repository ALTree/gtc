package main

import (
	"flag"
	"fmt"
	"os"

	"golang.org/x/exp/trace"
)

func main() {
	start := flag.Uint64("s", 0, "Start timestamp for verbose prints")
	end := flag.Uint64("e", 1<<63, "End timestamp for verbose prints")
	g := flag.Int64("g", -1, "Goroutine")
	t := flag.Int64("t", -1, "Thread")
	flag.Parse()

	file, err := os.Open(flag.Args()[0])
	if err != nil {
		panic(err)
	}

	tr, _ := trace.NewReader(file)
	var e trace.Event
	for err == nil {
		e, err = tr.ReadEvent()
		ts := uint64(e.Time())

		// time
		filter := *start <= ts && ts <= *end

		// G
		filter = filter && (*g == -1 || int64(e.Goroutine()) == *g)

		// Thread
		filter = filter && (*t == -1 || int64(e.Thread()) == *t)

		if filter {
			fmt.Println("|", e)
		}
	}
}
