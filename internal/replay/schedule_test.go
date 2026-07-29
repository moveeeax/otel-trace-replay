package replay

import (
	"testing"
	"time"

	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

func tracesOf(spans ...*tracepb.Span) *tracepb.TracesData {
	return &tracepb.TracesData{ResourceSpans: []*tracepb.ResourceSpans{{
		ScopeSpans: []*tracepb.ScopeSpans{{Spans: spans}},
	}}}
}

func spanAt(start uint64) *tracepb.Span {
	return &tracepb.Span{StartTimeUnixNano: start, EndTimeUnixNano: start + 1}
}

func TestFlattenSortsByStart(t *testing.T) {
	items := Flatten(tracesOf(spanAt(300), spanAt(100), spanAt(200)))
	if len(items) != 3 {
		t.Fatalf("len = %d, want 3", len(items))
	}
	if items[0].Span.GetStartTimeUnixNano() != 100 || items[2].Span.GetStartTimeUnixNano() != 300 {
		t.Fatalf("not sorted: %d..%d", items[0].Span.GetStartTimeUnixNano(), items[2].Span.GetStartTimeUnixNano())
	}
}

func TestBuildScheduleRelative(t *testing.T) {
	base := uint64(1_000_000_000)
	items := Flatten(tracesOf(spanAt(base), spanAt(base+500_000_000), spanAt(base+1_000_000_000)))
	sched := BuildSchedule(items, 1.0)
	want := []time.Duration{0, 500 * time.Millisecond, 1000 * time.Millisecond}
	for i, s := range sched {
		if s.Delay != want[i] {
			t.Fatalf("delay[%d] = %s, want %s", i, s.Delay, want[i])
		}
	}
}

func TestBuildScheduleScale(t *testing.T) {
	items := Flatten(tracesOf(spanAt(0), spanAt(1_000_000_000)))
	sched := BuildSchedule(items, 0.5)
	if sched[1].Delay != 500*time.Millisecond {
		t.Fatalf("scaled delay = %s, want 500ms", sched[1].Delay)
	}
}

func TestBuildScheduleBurst(t *testing.T) {
	items := Flatten(tracesOf(spanAt(0), spanAt(1_000_000_000)))
	for i, s := range BuildSchedule(items, 0) {
		if s.Delay != 0 {
			t.Fatalf("burst delay[%d] = %s, want 0", i, s.Delay)
		}
	}
}

func TestBuildScheduleEmpty(t *testing.T) {
	if got := BuildSchedule(nil, 1.0); len(got) != 0 {
		t.Fatalf("len = %d, want 0", len(got))
	}
}
