module github.com/ALTree/gtc

go 1.25.1

require (
	github.com/ALTree/perfetto v0.4.1-0.20251009141930-3895746c15ba
	golang.org/x/exp v0.0.0-20251002181428-27f1f14c8bb9
)

require google.golang.org/protobuf v1.36.10 // indirect

replace github.com/ALTree/perfetto => ../perfetto
