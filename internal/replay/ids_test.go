package replay

import (
	"bytes"
	"context"
	"testing"
	"time"

	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

var (
	traceA   = []byte{0xaa, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	external = []byte{0xee, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	rootID   = []byte{1, 0, 0, 0, 0, 0, 0, 1}
	childID  = []byte{1, 0, 0, 0, 0, 0, 0, 2}
	orphanP  = []byte{9, 9, 9, 9, 9, 9, 9, 9}
)

// linkedTrace is a root span, a child with a link back to the root, and a span
// whose parent and second link point outside the recording.
func linkedTrace() *tracepb.TracesData {
	root := &tracepb.Span{TraceId: traceA, SpanId: rootID, StartTimeUnixNano: 0}
	child := &tracepb.Span{
		TraceId: traceA, SpanId: childID, ParentSpanId: rootID, StartTimeUnixNano: 1,
		Links: []*tracepb.Span_Link{
			{TraceId: traceA, SpanId: rootID},
			{TraceId: external, SpanId: rootID},
		},
	}
	orphan := &tracepb.Span{TraceId: traceA, SpanId: []byte{3, 3, 3, 3, 3, 3, 3, 3}, ParentSpanId: orphanP, StartTimeUnixNano: 2}
	return tracesOf(root, child, orphan)
}

func emitted(exp *fakeExporter) []*tracepb.Span {
	var out []*tracepb.Span
	for _, r := range exp.reqs {
		out = append(out, r.GetResourceSpans()[0].GetScopeSpans()[0].GetSpans()[0])
	}
	return out
}

func TestRunNewIDsRewritesConsistently(t *testing.T) {
	data := linkedTrace()
	exp := &fakeExporter{}
	_, err := Run(context.Background(), data, exp, Options{
		Scale: 0, Times: 2, NewIDs: true,
		Sleep: func(context.Context, time.Duration) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	got := emitted(exp)
	if len(got) != 6 {
		t.Fatalf("emitted %d spans, want 6", len(got))
	}
	for pass := 0; pass < 2; pass++ {
		root, child, orphan := got[pass*3], got[pass*3+1], got[pass*3+2]
		if bytes.Equal(root.TraceId, traceA) || bytes.Equal(root.SpanId, rootID) {
			t.Fatalf("pass %d: root IDs not rewritten", pass)
		}
		if !bytes.Equal(child.TraceId, root.TraceId) || !bytes.Equal(orphan.TraceId, root.TraceId) {
			t.Fatalf("pass %d: spans of one trace got different trace IDs", pass)
		}
		if !bytes.Equal(child.ParentSpanId, root.SpanId) {
			t.Fatalf("pass %d: child parent %x, want new root ID %x", pass, child.ParentSpanId, root.SpanId)
		}
		if l := child.Links[0]; !bytes.Equal(l.TraceId, root.TraceId) || !bytes.Equal(l.SpanId, root.SpanId) {
			t.Fatalf("pass %d: internal link not rewritten: %x/%x", pass, l.TraceId, l.SpanId)
		}
		if l := child.Links[1]; !bytes.Equal(l.TraceId, external) || !bytes.Equal(l.SpanId, rootID) {
			t.Fatalf("pass %d: external link changed: %x/%x", pass, l.TraceId, l.SpanId)
		}
		if !bytes.Equal(orphan.ParentSpanId, orphanP) {
			t.Fatalf("pass %d: parent outside the recording changed: %x", pass, orphan.ParentSpanId)
		}
	}
	if bytes.Equal(got[0].TraceId, got[3].TraceId) || bytes.Equal(got[0].SpanId, got[3].SpanId) {
		t.Fatal("two passes share IDs")
	}
	// The recording itself must stay untouched for the next pass.
	if orig := data.ResourceSpans[0].ScopeSpans[0].Spans[1]; !bytes.Equal(orig.TraceId, traceA) || !bytes.Equal(orig.Links[0].SpanId, rootID) {
		t.Fatal("recorded span was mutated")
	}
}

func TestRunWithoutNewIDsKeepsIDs(t *testing.T) {
	exp := &fakeExporter{}
	_, err := Run(context.Background(), linkedTrace(), exp, Options{
		Scale: 0, Times: 2,
		Sleep: func(context.Context, time.Duration) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, sp := range emitted(exp) {
		if !bytes.Equal(sp.TraceId, traceA) {
			t.Fatalf("trace ID rewritten without NewIDs: %x", sp.TraceId)
		}
	}
}

func TestRandomIDSkipsAllZero(t *testing.T) {
	r := bytes.NewReader(append(make([]byte, 8), 1, 2, 3, 4, 5, 6, 7, 8))
	id, err := randomID(r, 8)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(id, []byte{1, 2, 3, 4, 5, 6, 7, 8}) {
		t.Fatalf("id = %x", id)
	}
}

func TestMappingsSorted(t *testing.T) {
	m, err := NewIDMap(Flatten(linkedTrace()))
	if err != nil {
		t.Fatal(err)
	}
	ms := m.Mappings()
	if len(ms) != 4 {
		t.Fatalf("len = %d, want 1 trace + 3 spans", len(ms))
	}
	if ms[0].Kind != "trace" || ms[0].Old != "aa0102030405060708090a0b0c0d0e0f" {
		t.Fatalf("first mapping = %+v", ms[0])
	}
	if ms[1].Old != "0100000000000001" || ms[2].Old != "0100000000000002" {
		t.Fatalf("span mappings not sorted: %+v", ms[1:])
	}
}
