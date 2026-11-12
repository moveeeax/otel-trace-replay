package replay

import (
	"context"
	"time"

	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

// Exporter sends one OTLP export request to a collector.
type Exporter interface {
	Export(ctx context.Context, req *coltracepb.ExportTraceServiceRequest) error
}

// Options configures a replay run. Sleep and Now are injectable for tests; when
// nil they use the real timer and clock.
type Options struct {
	Scale       float64 // emission timing multiplier; <= 0 = burst
	Times       int     // number of passes; ignored when Loop is set
	Loop        bool    // replay forever until the context is cancelled
	RewriteTime bool    // shift embedded timestamps so the trace starts "now"
	NewIDs      bool    // give every pass fresh trace and span IDs

	Sleep func(context.Context, time.Duration) error
	Now   func() time.Time
}

// Stats summarizes a completed run.
type Stats struct {
	Passes int
	Spans  int
}

// Run replays the trace to exp according to opts. It returns when every pass is
// done, the exporter errors, or the context is cancelled (a cancellation
// surfaces as ctx.Err()).
func Run(ctx context.Context, td *tracepb.TracesData, exp Exporter, opts Options) (Stats, error) {
	sleep := opts.Sleep
	if sleep == nil {
		sleep = waitFor
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}

	items := Flatten(td)
	sched := BuildSchedule(items, opts.Scale)

	var stats Stats
	passes := opts.Times
	if passes < 1 {
		passes = 1
	}
	for pass := 0; opts.Loop || pass < passes; pass++ {
		var delta int64
		if opts.RewriteTime && len(items) > 0 {
			delta = now().UnixNano() - int64(items[0].Span.GetStartTimeUnixNano())
		}
		var ids *IDMap
		if opts.NewIDs {
			var err error
			if ids, err = NewIDMap(items); err != nil {
				return stats, err
			}
		}
		prev := time.Duration(0)
		for _, s := range sched {
			if err := sleep(ctx, s.Delay-prev); err != nil {
				return stats, err
			}
			prev = s.Delay
			if err := exp.Export(ctx, buildRequest(s.Item, delta, ids)); err != nil {
				return stats, err
			}
			stats.Spans++
		}
		stats.Passes++
	}
	return stats, nil
}

func waitFor(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// buildRequest wraps a single item back into an OTLP export request, shifting
// its embedded timestamps by delta nanoseconds when rewriting time and mapping
// its IDs through ids when that is non-nil. The recorded span is never mutated.
func buildRequest(it Item, delta int64, ids *IDMap) *coltracepb.ExportTraceServiceRequest {
	sp := it.Span
	if delta != 0 || ids != nil {
		sp = proto.Clone(sp).(*tracepb.Span)
	}
	if delta != 0 {
		shiftSpan(sp, delta)
	}
	if ids != nil {
		ids.apply(sp)
	}
	return &coltracepb.ExportTraceServiceRequest{
		ResourceSpans: []*tracepb.ResourceSpans{{
			Resource: it.Resource,
			ScopeSpans: []*tracepb.ScopeSpans{{
				Scope: it.Scope,
				Spans: []*tracepb.Span{sp},
			}},
		}},
	}
}

// shiftSpan moves the timestamps of sp in place. sp must be a private copy.
func shiftSpan(c *tracepb.Span, delta int64) {
	if c.StartTimeUnixNano > 0 {
		c.StartTimeUnixNano = uint64(int64(c.StartTimeUnixNano) + delta)
	}
	if c.EndTimeUnixNano > 0 {
		c.EndTimeUnixNano = uint64(int64(c.EndTimeUnixNano) + delta)
	}
	for _, e := range c.Events {
		if e.TimeUnixNano > 0 {
			e.TimeUnixNano = uint64(int64(e.TimeUnixNano) + delta)
		}
	}
}
