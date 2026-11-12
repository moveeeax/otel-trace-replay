package replay

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"sort"

	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

// IDMap maps the trace and span IDs of a recording to fresh random IDs for one
// replay pass, so a backend sees every pass as a separate trace instead of
// merging or deduplicating them.
//
// Only IDs defined by spans in the recording are mapped. A parentSpanId or link
// that points outside the recording keeps its original value, so a link to a
// real trace already stored in the backend still resolves.
type IDMap struct {
	traces map[string][]byte  // old trace ID -> new trace ID
	spans  map[spanKey][]byte // (old trace ID, old span ID) -> new span ID
}

// spanKey scopes a span ID to its trace: span IDs are only unique within one.
type spanKey struct{ trace, span string }

// NewIDMap draws a fresh trace ID for every distinct trace and a fresh span ID
// for every span in items, reading randomness from crypto/rand.
func NewIDMap(items []Item) (*IDMap, error) {
	return newIDMap(items, rand.Reader)
}

func newIDMap(items []Item, r io.Reader) (*IDMap, error) {
	m := &IDMap{traces: map[string][]byte{}, spans: map[spanKey][]byte{}}
	for _, it := range items {
		tid, sid := it.Span.GetTraceId(), it.Span.GetSpanId()
		if len(tid) == 0 {
			continue
		}
		if _, ok := m.traces[string(tid)]; !ok {
			id, err := randomID(r, len(tid))
			if err != nil {
				return nil, err
			}
			m.traces[string(tid)] = id
		}
		if len(sid) == 0 {
			continue
		}
		k := spanKey{string(tid), string(sid)}
		if _, ok := m.spans[k]; !ok {
			id, err := randomID(r, len(sid))
			if err != nil {
				return nil, err
			}
			m.spans[k] = id
		}
	}
	return m, nil
}

// randomID returns n random bytes that are not all zero; the OTLP spec treats an
// all-zero trace or span ID as invalid.
func randomID(r io.Reader, n int) ([]byte, error) {
	b := make([]byte, n)
	for {
		if _, err := io.ReadFull(r, b); err != nil {
			return nil, fmt.Errorf("generate id: %w", err)
		}
		for _, x := range b {
			if x != 0 {
				return b, nil
			}
		}
	}
}

// apply rewrites the IDs of sp in place. sp must be a private copy.
func (m *IDMap) apply(sp *tracepb.Span) {
	tid := string(sp.TraceId)
	if len(sp.SpanId) > 0 {
		if id, ok := m.spans[spanKey{tid, string(sp.SpanId)}]; ok {
			sp.SpanId = id
		}
	}
	if len(sp.ParentSpanId) > 0 {
		if id, ok := m.spans[spanKey{tid, string(sp.ParentSpanId)}]; ok {
			sp.ParentSpanId = id
		}
	}
	if id, ok := m.traces[tid]; ok {
		sp.TraceId = id
	}
	for _, l := range sp.Links {
		ltid := string(l.TraceId)
		if id, ok := m.spans[spanKey{ltid, string(l.SpanId)}]; ok {
			l.SpanId = id
		}
		if id, ok := m.traces[ltid]; ok {
			l.TraceId = id
		}
	}
}

// Mapping is one old-to-new ID pair in hex, as shown by --dry-run.
type Mapping struct {
	Kind string // "trace" or "span"
	Old  string
	New  string
}

// Mappings lists every pair, traces first, each group sorted by old ID so the
// output is stable.
func (m *IDMap) Mappings() []Mapping {
	var traces, spans []Mapping
	for old, id := range m.traces {
		traces = append(traces, Mapping{"trace", hex.EncodeToString([]byte(old)), hex.EncodeToString(id)})
	}
	for k, id := range m.spans {
		spans = append(spans, Mapping{"span", hex.EncodeToString([]byte(k.span)), hex.EncodeToString(id)})
	}
	byOld := func(s []Mapping) {
		sort.Slice(s, func(i, j int) bool { return s[i].Old < s[j].Old })
	}
	byOld(traces)
	byOld(spans)
	return append(traces, spans...)
}
