package replay

import (
	"context"
	"testing"
	"time"

	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
)

type fakeExporter struct {
	reqs []*coltracepb.ExportTraceServiceRequest
}

func (f *fakeExporter) Export(_ context.Context, req *coltracepb.ExportTraceServiceRequest) error {
	f.reqs = append(f.reqs, req)
	return nil
}

func firstStart(req *coltracepb.ExportTraceServiceRequest) uint64 {
	return req.GetResourceSpans()[0].GetScopeSpans()[0].GetSpans()[0].GetStartTimeUnixNano()
}

func TestRunEmitsSpansInOrder(t *testing.T) {
	base := uint64(1_000_000_000)
	data := tracesOf(spanAt(base+200_000_000), spanAt(base), spanAt(base+100_000_000))
	exp := &fakeExporter{}
	var slept []time.Duration
	stats, err := Run(context.Background(), data, exp, Options{
		Scale: 1.0, Times: 1,
		Sleep: func(_ context.Context, d time.Duration) error { slept = append(slept, d); return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Spans != 3 || stats.Passes != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	got := []uint64{firstStart(exp.reqs[0]), firstStart(exp.reqs[1]), firstStart(exp.reqs[2])}
	want := []uint64{base, base + 100_000_000, base + 200_000_000}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("emit order[%d] = %d, want %d", i, got[i], want[i])
		}
	}
	// Incremental sleeps between three spans 100ms apart: 0, 100ms, 100ms.
	wantSleep := []time.Duration{0, 100 * time.Millisecond, 100 * time.Millisecond}
	if len(slept) != 3 {
		t.Fatalf("sleeps = %v", slept)
	}
	for i := range wantSleep {
		if slept[i] != wantSleep[i] {
			t.Fatalf("sleep[%d] = %s, want %s", i, slept[i], wantSleep[i])
		}
	}
}

func TestRunMultiplePasses(t *testing.T) {
	data := tracesOf(spanAt(0), spanAt(1))
	exp := &fakeExporter{}
	stats, err := Run(context.Background(), data, exp, Options{
		Scale: 0, Times: 3,
		Sleep: func(context.Context, time.Duration) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Passes != 3 || stats.Spans != 6 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestRunRewriteTimeAnchorsToNow(t *testing.T) {
	base := uint64(1_000_000_000)
	data := tracesOf(spanAt(base), spanAt(base+500_000_000))
	exp := &fakeExporter{}
	fixedNow := time.Unix(2000, 0)
	_, err := Run(context.Background(), data, exp, Options{
		Scale: 0, Times: 1, RewriteTime: true,
		Now:   func() time.Time { return fixedNow },
		Sleep: func(context.Context, time.Duration) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	first := firstStart(exp.reqs[0])
	if int64(first) != fixedNow.UnixNano() {
		t.Fatalf("first span start = %d, want %d", first, fixedNow.UnixNano())
	}
	second := firstStart(exp.reqs[1])
	if int64(second)-int64(first) != 500_000_000 {
		t.Fatalf("relative offset not preserved: %d", int64(second)-int64(first))
	}
}

func TestRunStopsOnContextError(t *testing.T) {
	data := tracesOf(spanAt(0), spanAt(1_000_000_000))
	exp := &fakeExporter{}
	_, err := Run(context.Background(), data, exp, Options{
		Scale: 1.0, Times: 1,
		Sleep: func(context.Context, time.Duration) error { return context.Canceled },
	})
	if err != context.Canceled {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if len(exp.reqs) != 0 {
		t.Fatalf("emitted %d requests after cancel, want 0", len(exp.reqs))
	}
}
