// Package replay turns a recorded trace into a timed stream of OTLP export
// requests and drives them to an exporter.
package replay

import (
	"sort"
	"time"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

// Item is one span carrying enough resource and scope context to be re-emitted
// on its own without losing the resource attributes dashboards group by.
type Item struct {
	Resource *resourcepb.Resource
	Scope    *commonpb.InstrumentationScope
	Span     *tracepb.Span
}

// Scheduled pairs an item with the delay, measured from replay start, at which
// it should be emitted.
type Scheduled struct {
	Delay time.Duration
	Item  Item
}

// Flatten collects every span into a single slice sorted by start time, so the
// replay follows the original wall-clock order across resources and scopes.
func Flatten(td *tracepb.TracesData) []Item {
	var items []Item
	for _, rs := range td.GetResourceSpans() {
		for _, ss := range rs.GetScopeSpans() {
			for _, sp := range ss.GetSpans() {
				items = append(items, Item{
					Resource: rs.GetResource(),
					Scope:    ss.GetScope(),
					Span:     sp,
				})
			}
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].Span.GetStartTimeUnixNano() < items[j].Span.GetStartTimeUnixNano()
	})
	return items
}

// BuildSchedule computes each item's emission delay relative to the earliest
// span start, scaled by scale. A scale <= 0 collapses every delay to zero, i.e.
// burst mode: emit as fast as the exporter accepts.
func BuildSchedule(items []Item, scale float64) []Scheduled {
	sched := make([]Scheduled, 0, len(items))
	if len(items) == 0 {
		return sched
	}
	min := items[0].Span.GetStartTimeUnixNano()
	for _, it := range items {
		if s := it.Span.GetStartTimeUnixNano(); s < min {
			min = s
		}
	}
	for _, it := range items {
		var d time.Duration
		if scale > 0 {
			d = time.Duration(float64(it.Span.GetStartTimeUnixNano()-min) * scale)
		}
		sched = append(sched, Scheduled{Delay: d, Item: it})
	}
	return sched
}
