// Package trace loads recorded OTLP/JSON trace files into OTLP protobuf types.
package trace

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

// Load reads an OTLP/JSON trace file — the {"resourceSpans": [...]} document
// emitted by the collector's file exporter — into TracesData.
func Load(path string) (*tracepb.TracesData, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	td, err := Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return td, nil
}

// Parse unmarshals an OTLP/JSON document into TracesData. Unknown fields are
// discarded so traces recorded by newer collectors still load.
//
// It also accepts JSON Lines, one TracesData document per line, which is what
// the collector's file exporter writes when it appends batch after batch; the
// resourceSpans of every line are concatenated.
func Parse(raw []byte) (*tracepb.TracesData, error) {
	td := &tracepb.TracesData{}
	err := parseDoc(raw, td)
	if err == nil {
		return td, nil
	}
	lines := bytes.Split(raw, []byte("\n"))
	if len(bytes.TrimSpace(raw)) == 0 || countNonEmpty(lines) < 2 {
		return nil, fmt.Errorf("parse OTLP JSON: %w", err)
	}
	td = &tracepb.TracesData{}
	first := true
	for i, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		part := &tracepb.TracesData{}
		if lerr := parseDoc(line, part); lerr != nil {
			if first {
				// Not JSON Lines either: report the whole-document error.
				return nil, fmt.Errorf("parse OTLP JSON: %w", err)
			}
			return nil, fmt.Errorf("parse OTLP JSON line %d: %w", i+1, lerr)
		}
		first = false
		td.ResourceSpans = append(td.ResourceSpans, part.ResourceSpans...)
	}
	return td, nil
}

// parseDoc unmarshals one OTLP/JSON document. OTLP/JSON encodes trace and span
// IDs as hex strings, while protojson follows the standard Protobuf JSON
// mapping and would read them as base64, so the IDs are converted first.
func parseDoc(raw []byte, td *tracepb.TracesData) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber() // keep 64-bit integers exact through the round trip
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return err
	}
	if dec.More() {
		return fmt.Errorf("unexpected data after the JSON document")
	}
	hexIDsToBase64(doc)
	norm, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	return protojson.UnmarshalOptions{DiscardUnknown: true}.Unmarshal(norm, td)
}

// idFields are the OTLP byte fields that OTLP/JSON writes as hex, in both the
// lowerCamelCase and the original proto field spelling protojson accepts.
var idFields = map[string]bool{
	"traceId": true, "spanId": true, "parentSpanId": true,
	"trace_id": true, "span_id": true, "parent_span_id": true,
}

// hexIDsToBase64 rewrites, in place, every hex-encoded ID field in a decoded
// JSON tree to base64. A value that is not valid hex is left alone, so a file
// that already uses base64 still loads: base64 of an 8- or 16-byte ID always
// ends in "=" padding and so is never valid hex.
func hexIDsToBase64(v any) {
	switch n := v.(type) {
	case map[string]any:
		for k, child := range n {
			if s, ok := child.(string); ok && idFields[k] {
				if b, err := hex.DecodeString(s); err == nil {
					n[k] = base64.StdEncoding.EncodeToString(b)
				}
				continue
			}
			hexIDsToBase64(child)
		}
	case []any:
		for _, child := range n {
			hexIDsToBase64(child)
		}
	}
}

func countNonEmpty(lines [][]byte) int {
	n := 0
	for _, l := range lines {
		if len(bytes.TrimSpace(l)) > 0 {
			n++
		}
	}
	return n
}

// CountSpans returns the total number of spans across every resource and scope.
func CountSpans(td *tracepb.TracesData) int {
	n := 0
	for _, rs := range td.GetResourceSpans() {
		for _, ss := range rs.GetScopeSpans() {
			n += len(ss.GetSpans())
		}
	}
	return n
}
