package diff

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

const ms = uint64(time.Millisecond)

// sp builds a span of trace tid with a one-byte span ID and parent (0 = root);
// start and end are in milliseconds.
func sp(tid, id, parent byte, name string, start, end uint64) *tracepb.Span {
	s := &tracepb.Span{
		TraceId:           []byte{tid, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
		SpanId:            []byte{id, 0, 0, 0, 0, 0, 0, 1},
		Name:              name,
		StartTimeUnixNano: start * ms,
		EndTimeUnixNano:   end * ms,
	}
	if parent != 0 {
		s.ParentSpanId = []byte{parent, 0, 0, 0, 0, 0, 0, 1}
	}
	return s
}

func data(spans ...*tracepb.Span) *tracepb.TracesData {
	return &tracepb.TracesData{ResourceSpans: []*tracepb.ResourceSpans{{
		ScopeSpans: []*tracepb.ScopeSpans{{Spans: spans}},
	}}}
}

func rowFor(t *testing.T, r Report, path string) Row {
	t.Helper()
	for _, row := range r.Rows {
		if row.Path == path {
			return row
		}
	}
	t.Fatalf("no row for %q in %+v", path, r.Rows)
	return Row{}
}

func TestSelfTimeMergesOverlapAndClips(t *testing.T) {
	parent := sp(1, 1, 0, "p", 0, 100)
	kids := []*tracepb.Span{
		sp(1, 2, 1, "a", 10, 30),
		sp(1, 3, 1, "b", 20, 40),  // overlaps a: 10..40 covered once
		sp(1, 4, 1, "c", 90, 120), // clipped to 90..100; covered 30+10 of 100
	}
	if got, want := selfTime(parent, kids), 60*time.Millisecond; got != want {
		t.Fatalf("self = %s, want %s", got, want)
	}
}

func TestCompareNamesTheSlowerSpan(t *testing.T) {
	base := Aggregate(data(
		sp(1, 1, 0, "POST /checkout", 0, 500),
		sp(1, 2, 1, "db", 100, 200),
	))
	// Different IDs on purpose: alignment is by path, not by ID.
	cand := Aggregate(data(
		sp(9, 7, 0, "POST /checkout", 0, 880),
		sp(9, 8, 7, "db", 100, 580),
	))
	r := Compare(base, cand, 0)
	if r.DurationDelta != 380*time.Millisecond {
		t.Fatalf("duration delta = %s", r.DurationDelta)
	}
	top := r.Rows[0]
	if top.Path != "POST /checkout > db" || top.SelfDelta != 380*time.Millisecond || top.Change != Slower {
		t.Fatalf("top row = %+v", top)
	}
	if top.CountEffect != 0 || top.PerCallEffect != 380*time.Millisecond {
		t.Fatalf("effects = %s / %s", top.CountEffect, top.PerCallEffect)
	}
	if math.Abs(top.Share-1) > 1e-9 {
		t.Fatalf("share = %f, want 1", top.Share)
	}
	// Root self time is unchanged (400ms both sides); its wall time grew.
	root := rowFor(t, r, "POST /checkout")
	if root.SelfDelta != 0 || root.WallDelta != 380*time.Millisecond || root.Change != ViaChildren {
		t.Fatalf("root row = %+v", root)
	}
}

func TestCompareSeparatesMoreCallsFromSlower(t *testing.T) {
	base := Aggregate(data(
		sp(1, 1, 0, "GET /cart", 0, 100),
		sp(1, 2, 1, "SELECT item", 10, 20),
	))
	cand := Aggregate(data(
		sp(1, 1, 0, "GET /cart", 0, 140),
		sp(1, 2, 1, "SELECT item", 10, 20),
		sp(1, 3, 1, "SELECT item", 20, 30),
		sp(1, 4, 1, "SELECT item", 30, 40),
		sp(1, 5, 1, "SELECT item", 40, 50),
		sp(1, 6, 1, "SELECT item", 50, 60),
	))
	row := rowFor(t, Compare(base, cand, 0), "GET /cart > SELECT item")
	if row.Change != MoreCalls {
		t.Fatalf("change = %s, want %s", row.Change, MoreCalls)
	}
	if row.Baseline.Calls != 1 || row.Candidate.Calls != 5 {
		t.Fatalf("calls = %v -> %v", row.Baseline.Calls, row.Candidate.Calls)
	}
	if row.CountEffect != 40*time.Millisecond || row.PerCallEffect != 0 {
		t.Fatalf("effects = %s / %s", row.CountEffect, row.PerCallEffect)
	}
}

func TestCompareNewAndGone(t *testing.T) {
	base := Aggregate(data(sp(1, 1, 0, "root", 0, 100), sp(1, 2, 1, "cache", 0, 5)))
	cand := Aggregate(data(sp(1, 1, 0, "root", 0, 100), sp(1, 2, 1, "db", 0, 50)))
	r := Compare(base, cand, 0)
	if got := rowFor(t, r, "root > db").Change; got != New {
		t.Fatalf("db change = %s", got)
	}
	if got := rowFor(t, r, "root > cache").Change; got != Gone {
		t.Fatalf("cache change = %s", got)
	}
}

func TestAggregateUsesMedianAcrossTraces(t *testing.T) {
	// Three baseline traces; the third is an outlier at 1s.
	base := Aggregate(
		data(sp(1, 1, 0, "op", 0, 100), sp(2, 1, 0, "op", 0, 100)),
		data(sp(3, 1, 0, "op", 0, 1000)),
	)
	if base.Traces != 3 {
		t.Fatalf("traces = %d, want 3", base.Traces)
	}
	if got := base.Paths["op"].Wall; got != 100*time.Millisecond {
		t.Fatalf("median wall = %s, want 100ms", got)
	}
	if base.Duration != 100*time.Millisecond {
		t.Fatalf("median duration = %s", base.Duration)
	}
}

func TestAggregateCountsMissingPathAsZero(t *testing.T) {
	// "retry" shows up in one trace out of three: the median is zero calls.
	side := Aggregate(data(
		sp(1, 1, 0, "op", 0, 10), sp(1, 2, 1, "retry", 0, 5),
		sp(2, 1, 0, "op", 0, 10),
		sp(3, 1, 0, "op", 0, 10),
	))
	if got := side.Paths["op > retry"].Calls; got != 0 {
		t.Fatalf("median calls = %v, want 0", got)
	}
}

func TestCompareMinDelta(t *testing.T) {
	base := Aggregate(data(
		sp(1, 1, 0, "root", 0, 100),
		sp(1, 2, 1, "small", 0, 10),
		sp(1, 3, 1, "tiny", 20, 21),
	))
	cand := Aggregate(data(
		sp(1, 1, 0, "root", 0, 100),
		sp(1, 2, 1, "small", 0, 15),
		sp(1, 3, 1, "tiny", 20, 21),
		sp(1, 4, 1, "tiny", 22, 23),
	))
	r := Compare(base, cand, 25*time.Millisecond)
	if len(r.Rows) != 1 || r.Rows[0].Path != "root > tiny" {
		t.Fatalf("rows = %+v, want only the span whose call count changed", r.Rows)
	}
}

func TestCompareSkipsUnchanged(t *testing.T) {
	d := data(sp(1, 1, 0, "root", 0, 100), sp(1, 2, 1, "child", 10, 20))
	if r := Compare(Aggregate(d), Aggregate(d), 0); len(r.Rows) != 0 {
		t.Fatalf("rows = %+v, want none", r.Rows)
	}
}

func TestParentCycleTerminates(t *testing.T) {
	a := sp(1, 1, 2, "a", 0, 10)
	b := sp(1, 2, 1, "b", 0, 10)
	side := Aggregate(data(a, b))
	if len(side.Paths) != 2 {
		t.Fatalf("paths = %v", side.Paths)
	}
}

func TestWriteTableAndJSON(t *testing.T) {
	base := Aggregate(data(sp(1, 1, 0, "op", 0, 100), sp(1, 2, 1, "db", 10, 20)))
	cand := Aggregate(data(sp(1, 1, 0, "op", 0, 480), sp(1, 2, 1, "db", 10, 400)))
	r := Compare(base, cand, 0)

	var tbl bytes.Buffer
	if err := WriteTable(&tbl, r); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"median duration 100ms", "(+380ms)", "+380ms", "1->1", "slower", "op > db"} {
		if !strings.Contains(tbl.String(), want) {
			t.Fatalf("table missing %q:\n%s", want, tbl.String())
		}
	}

	var js bytes.Buffer
	if err := WriteJSON(&js, r); err != nil {
		t.Fatal(err)
	}
	var got struct {
		DurationDeltaNs int64 `json:"duration_delta_ns"`
		Spans           []struct {
			Path        string   `json:"path"`
			Change      string   `json:"change"`
			SelfDeltaNs int64    `json:"self_delta_ns"`
			Share       *float64 `json:"share"`
		} `json:"spans"`
	}
	if err := json.Unmarshal(js.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.DurationDeltaNs != int64(380*time.Millisecond) || got.Spans[0].Path != "op > db" ||
		got.Spans[0].Change != "slower" || got.Spans[0].SelfDeltaNs != int64(380*time.Millisecond) || got.Spans[0].Share == nil {
		t.Fatalf("json = %s", js.String())
	}
}

func TestWriteTableNoRows(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteTable(&buf, Report{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "no span moved") {
		t.Fatalf("output = %s", buf.String())
	}
}
