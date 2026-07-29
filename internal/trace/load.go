// Package trace loads recorded OTLP/JSON trace files into OTLP protobuf types.
package trace

import (
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
func Parse(raw []byte) (*tracepb.TracesData, error) {
	td := &tracepb.TracesData{}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(raw, td); err != nil {
		return nil, fmt.Errorf("parse OTLP JSON: %w", err)
	}
	return td, nil
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
