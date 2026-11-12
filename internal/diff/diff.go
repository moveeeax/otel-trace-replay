// Package diff compares recorded traces of the same operation and reports where
// the time moved between a baseline and a candidate.
//
// Spans are aligned by their path in the span tree (the chain of span names from
// the root, e.g. "POST /checkout > charge-card"), never by span ID, so two
// recordings with unrelated IDs still line up. Each side may hold many traces;
// every per-span figure is the median across that side's traces, so a single
// unlucky request does not decide the result.
package diff

import (
	"math"
	"sort"
	"time"

	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

// PathSep joins span names into a path.
const PathSep = " > "

// maxDepth bounds the parent walk, so a recording with a parentSpanId cycle
// cannot loop forever.
const maxDepth = 256

// Side is one aggregated set of traces: the baseline or the candidate.
type Side struct {
	Traces   int           // number of distinct traces
	Duration time.Duration // median trace duration (latest end - earliest start)
	Paths    map[string]Stat
}

// Stat is the per-trace median for one span path.
type Stat struct {
	Calls float64       // spans with this path per trace
	Wall  time.Duration // summed span durations per trace
	Self  time.Duration // summed self time per trace: duration minus time covered by children
}

// perTrace holds one trace's totals for a path before the medians are taken.
type perTrace struct {
	calls      float64
	wall, self time.Duration
}

// Aggregate groups the spans of every input by trace ID and computes the
// per-path medians. Spans of one trace may be spread across inputs, resources
// and scopes.
func Aggregate(inputs ...*tracepb.TracesData) Side {
	byTrace := map[string][]*tracepb.Span{}
	var order []string
	for _, td := range inputs {
		for _, rs := range td.GetResourceSpans() {
			for _, ss := range rs.GetScopeSpans() {
				for _, sp := range ss.GetSpans() {
					k := string(sp.GetTraceId())
					if _, ok := byTrace[k]; !ok {
						order = append(order, k)
					}
					byTrace[k] = append(byTrace[k], sp)
				}
			}
		}
	}

	side := Side{Traces: len(order), Paths: map[string]Stat{}}
	durations := make([]float64, 0, len(order))
	samples := map[string][]perTrace{}
	for i, k := range order {
		dur, paths := analyzeTrace(byTrace[k])
		durations = append(durations, float64(dur))
		for p, v := range paths {
			if samples[p] == nil {
				// Traces before this one did not have the path: they count as zero.
				samples[p] = make([]perTrace, i, len(order))
			}
			samples[p] = append(samples[p], v)
		}
		for p := range samples {
			if len(samples[p]) == i {
				samples[p] = append(samples[p], perTrace{})
			}
		}
	}
	side.Duration = time.Duration(median(durations))
	for p, vs := range samples {
		calls := make([]float64, len(vs))
		wall := make([]float64, len(vs))
		self := make([]float64, len(vs))
		for i, v := range vs {
			calls[i], wall[i], self[i] = v.calls, float64(v.wall), float64(v.self)
		}
		side.Paths[p] = Stat{
			Calls: median(calls),
			Wall:  time.Duration(median(wall)),
			Self:  time.Duration(median(self)),
		}
	}
	return side
}

// analyzeTrace returns the trace duration and the per-path totals of one trace.
func analyzeTrace(spans []*tracepb.Span) (time.Duration, map[string]perTrace) {
	byID := make(map[string]*tracepb.Span, len(spans))
	children := map[string][]*tracepb.Span{}
	var minStart, maxEnd uint64
	for i, sp := range spans {
		if id := string(sp.GetSpanId()); id != "" {
			byID[id] = sp
		}
		if i == 0 || sp.GetStartTimeUnixNano() < minStart {
			minStart = sp.GetStartTimeUnixNano()
		}
		if sp.GetEndTimeUnixNano() > maxEnd {
			maxEnd = sp.GetEndTimeUnixNano()
		}
	}
	for _, sp := range spans {
		if p := string(sp.GetParentSpanId()); p != "" {
			if _, ok := byID[p]; ok {
				children[p] = append(children[p], sp)
			}
		}
	}

	memo := map[*tracepb.Span]string{}
	var pathOf func(sp *tracepb.Span, depth int) string
	pathOf = func(sp *tracepb.Span, depth int) string {
		if p, ok := memo[sp]; ok {
			return p
		}
		p := sp.GetName()
		if parent, ok := byID[string(sp.GetParentSpanId())]; ok && parent != sp && depth < maxDepth {
			p = pathOf(parent, depth+1) + PathSep + p
		}
		memo[sp] = p
		return p
	}

	out := map[string]perTrace{}
	for _, sp := range spans {
		p := pathOf(sp, 0)
		v := out[p]
		v.calls++
		v.wall += spanDuration(sp)
		v.self += selfTime(sp, children[string(sp.GetSpanId())])
		out[p] = v
	}
	var dur time.Duration
	if maxEnd > minStart {
		dur = time.Duration(maxEnd - minStart)
	}
	return dur, out
}

func spanDuration(sp *tracepb.Span) time.Duration {
	if sp.GetEndTimeUnixNano() <= sp.GetStartTimeUnixNano() {
		return 0
	}
	return time.Duration(sp.GetEndTimeUnixNano() - sp.GetStartTimeUnixNano())
}

// selfTime is the span duration minus the union of its children's intervals,
// clipped to the span. Overlapping (parallel) children are counted once.
func selfTime(sp *tracepb.Span, kids []*tracepb.Span) time.Duration {
	start, end := sp.GetStartTimeUnixNano(), sp.GetEndTimeUnixNano()
	if end <= start {
		return 0
	}
	type iv struct{ s, e uint64 }
	ivs := make([]iv, 0, len(kids))
	for _, k := range kids {
		s, e := max(k.GetStartTimeUnixNano(), start), min(k.GetEndTimeUnixNano(), end)
		if e > s {
			ivs = append(ivs, iv{s, e})
		}
	}
	sort.Slice(ivs, func(i, j int) bool { return ivs[i].s < ivs[j].s })
	var covered, curS, curE uint64
	for i, v := range ivs {
		if i == 0 || v.s > curE {
			covered += curE - curS
			curS, curE = v.s, v.e
		} else if v.e > curE {
			curE = v.e
		}
	}
	covered += curE - curS
	return time.Duration(end - start - covered)
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	m := len(s) / 2
	if len(s)%2 == 1 {
		return s[m]
	}
	return (s[m-1] + s[m]) / 2
}

// Change names what happened to a span path.
type Change string

// Change values. MoreCalls/FewerCalls mean the count change explains more of
// the self-time delta than the per-call change; Slower/Faster mean the opposite.
const (
	New        Change = "new"
	Gone       Change = "gone"
	MoreCalls  Change = "more calls"
	FewerCalls Change = "fewer calls"
	Slower     Change = "slower"
	Faster     Change = "faster"
	// ViaChildren: the span's own (self) time did not change, only the time
	// spent in its children, which have rows of their own.
	ViaChildren Change = "via children"
	Same        Change = "same"
)

// Row is the comparison of one span path.
type Row struct {
	Path      string
	Change    Change
	Baseline  Stat
	Candidate Stat
	SelfDelta time.Duration
	WallDelta time.Duration
	// CountEffect + PerCallEffect == SelfDelta. CountEffect is the self time
	// added or removed by the change in calls at the baseline cost per call;
	// PerCallEffect is the change in cost per call at the candidate call count.
	CountEffect   time.Duration
	PerCallEffect time.Duration
	// Share is SelfDelta as a fraction of the change in median trace duration;
	// NaN when the trace duration did not change.
	Share float64
}

// Report is the full comparison.
type Report struct {
	Baseline, Candidate Side
	DurationDelta       time.Duration
	Rows                []Row
}

// Compare aligns base and cand by span path. A path is reported when its call
// count changed (including paths that appeared or disappeared), or when its
// self or wall time moved by at least minDelta (and by more than zero). Rows
// are sorted by the size of the self-time delta, largest first.
func Compare(base, cand Side, minDelta time.Duration) Report {
	r := Report{Baseline: base, Candidate: cand, DurationDelta: cand.Duration - base.Duration}
	paths := map[string]bool{}
	for p := range base.Paths {
		paths[p] = true
	}
	for p := range cand.Paths {
		paths[p] = true
	}
	for p := range paths {
		b, c := base.Paths[p], cand.Paths[p]
		if b.Calls == 0 && c.Calls == 0 {
			continue // present only in a minority of traces on both sides
		}
		row := Row{
			Path: p, Baseline: b, Candidate: c,
			SelfDelta: c.Self - b.Self,
			WallDelta: c.Wall - b.Wall,
			Share:     math.NaN(),
		}
		var basePerCall float64
		if b.Calls > 0 {
			basePerCall = float64(b.Self) / b.Calls
		}
		row.CountEffect = time.Duration((c.Calls - b.Calls) * basePerCall)
		row.PerCallEffect = row.SelfDelta - row.CountEffect
		if r.DurationDelta != 0 {
			row.Share = float64(row.SelfDelta) / float64(r.DurationDelta)
		}
		row.Change = classify(row)

		moved := func(d time.Duration) bool { return d != 0 && abs(d) >= minDelta }
		if b.Calls != c.Calls || moved(row.SelfDelta) || moved(row.WallDelta) {
			r.Rows = append(r.Rows, row)
		}
	}
	sort.Slice(r.Rows, func(i, j int) bool {
		a, b := r.Rows[i], r.Rows[j]
		if abs(a.SelfDelta) != abs(b.SelfDelta) {
			return abs(a.SelfDelta) > abs(b.SelfDelta)
		}
		if abs(a.WallDelta) != abs(b.WallDelta) {
			return abs(a.WallDelta) > abs(b.WallDelta)
		}
		return a.Path < b.Path
	})
	return r
}

func classify(r Row) Change {
	switch {
	case r.Baseline.Calls == 0:
		return New
	case r.Candidate.Calls == 0:
		return Gone
	case r.Baseline.Calls != r.Candidate.Calls && abs(r.CountEffect) >= abs(r.PerCallEffect):
		if r.Candidate.Calls > r.Baseline.Calls {
			return MoreCalls
		}
		return FewerCalls
	}
	switch d := r.PerCallEffect; {
	case d > 0:
		return Slower
	case d < 0:
		return Faster
	case r.WallDelta != 0:
		return ViaChildren
	}
	return Same
}

func abs(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
