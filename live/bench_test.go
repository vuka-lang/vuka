package live_test

import (
	"context"
	"encoding/json"
	"runtime"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/vuka-lang/vuka"
	"github.com/vuka-lang/vuka/live"
	"github.com/vuka-lang/vuka/live/internal/demo"
)

func joined(b testing.TB, page func(context.Context) vuka.Node, v int) *live.Session {
	s := live.NewSession(context.Background(), page)
	out := s.HandleJSON([]byte(`{"type":"join","v":` + strconv.Itoa(v) + `}`))
	if len(out) < 20 {
		b.Fatalf("join: %s", out)
	}
	return s
}

func event(b testing.TB, s *live.Session, target string) int {
	out := s.HandleJSON([]byte(`{"type":"event","target":"` + target + `","event":"click"}`))
	if len(out) > 8 && string(out[:17]) == `{"type":"error","` {
		b.Fatalf("event: %s", out)
	}
	return len(out)
}

// bench clicks targets(i) in turn, reporting the bytes of each reply.
func bench(b *testing.B, page func(context.Context) vuka.Node, v int, target func(i int) string) {
	defer func(v bool) { live.Verify = v }(live.Verify)
	live.Verify = false
	s := joined(b, page, v)
	bytes := 0
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		bytes += event(b, s, target(i))
	}
	b.ReportMetric(float64(bytes)/float64(b.N), "B/patch")
}

func versions(b *testing.B, page func(context.Context) vuka.Node, target func(i int) string) {
	for _, v := range []int{1, 2} {
		b.Run("v"+strconv.Itoa(v), func(b *testing.B) { bench(b, page, v, target) })
	}
}

// A 1000-row table rendered by one component, one cell changing per event.
func BenchmarkCellTable(b *testing.B) {
	page := func(context.Context) vuka.Node { return vuka.Component("b#1", nil, &demo.CellTable{N: 1000}) }
	versions(b, page, func(i int) string { return "c1:" + strconv.Itoa(i*37%1000) })
}

// A 1000-row table of row components, one row changing per event: rows with
// Assign state are skipped unless clicked; plain rows render every time.
func BenchmarkRowTable(b *testing.B) {
	for _, plain := range []bool{false, true} {
		name := map[bool]string{false: "assign", true: "plain"}[plain]
		page := func(context.Context) vuka.Node { return vuka.Component("b#1", nil, &demo.Table{N: 1000, Plain: plain}) }
		b.Run(name, func(b *testing.B) {
			versions(b, page, func(i int) string { return "c" + strconv.Itoa(2+i*37%1000) + ":0" })
		})
	}
}

// A page of 100 counters, one clicked per event.
func BenchmarkCounters(b *testing.B) {
	versions(b, demo.Counters(100), func(i int) string { return "c" + strconv.Itoa(1+i*7%100) + ":0" })
}

// BenchmarkLoad is a scaled-down load test: n sessions of the demo page
// (two boards and a counter), each joined over v2, then events spread over
// them from GOMAXPROCS goroutines. It reports each event's latency (p50,
// p99) and the heap each session holds.
func BenchmarkLoad(b *testing.B) {
	for _, n := range []int{1000, 5000} {
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			defer func(v bool) { live.Verify = v }(live.Verify)
			live.Verify = false
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			ss := make([]*live.Session, n)
			for i := range ss {
				ss[i] = joined(b, demo.Page, 2)
			}
			runtime.GC()
			runtime.ReadMemStats(&after)
			perSession := float64(after.HeapAlloc-before.HeapAlloc) / float64(n)
			// Each board's input and counters: the handlers events go to.
			targets := []string{"c1:0", "c1:1", "c3:0", "c4:0", "c2:0", "c7:0"}
			words := []string{"add", "rotate", "rename", "big", "pop", "note", "front", "drop"}
			workers := runtime.GOMAXPROCS(0)
			lat := make([][]time.Duration, workers)
			b.ResetTimer()
			var wg sync.WaitGroup
			per := b.N/workers + 1
			for w := range workers {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for k := range per {
						i := w*per + k
						s := ss[i*7919%n]
						msg, _ := json.Marshal(live.ClientMessage{Type: live.EventMsg, Event: vuka.Event{Target: targets[i%len(targets)], Type: "input", Value: words[i%len(words)]}})
						t0 := time.Now()
						s.HandleJSON(msg)
						lat[w] = append(lat[w], time.Since(t0))
					}
				}()
			}
			wg.Wait()
			b.StopTimer()
			all := slices.Concat(lat...)
			slices.Sort(all)
			b.ReportMetric(float64(all[len(all)/2].Microseconds()), "p50-µs")
			b.ReportMetric(float64(all[len(all)*99/100].Microseconds()), "p99-µs")
			b.ReportMetric(perSession, "B/session")
			runtime.KeepAlive(ss)
		})
	}
}
