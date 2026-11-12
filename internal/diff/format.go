package diff

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"text/tabwriter"
	"time"
)

// WriteTable prints the report as an aligned text table.
func WriteTable(w io.Writer, r Report) error {
	fmt.Fprintf(w, "baseline:  %d trace(s), median duration %s\n", r.Baseline.Traces, round(r.Baseline.Duration))
	fmt.Fprintf(w, "candidate: %d trace(s), median duration %s (%s)\n\n", r.Candidate.Traces, round(r.Candidate.Duration), signed(r.DurationDelta))
	if len(r.Rows) == 0 {
		_, err := fmt.Fprintln(w, "no span moved by the minimum delta")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SELF\tWALL\tCALLS\tFROM CALLS\tFROM PER-CALL\tSHARE\tCHANGE\tSPAN")
	for _, row := range r.Rows {
		share := "-"
		if !math.IsNaN(row.Share) {
			share = fmt.Sprintf("%.0f%%", row.Share*100)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s->%s\t%s\t%s\t%s\t%s\t%s\n",
			signed(row.SelfDelta), signed(row.WallDelta),
			calls(row.Baseline.Calls), calls(row.Candidate.Calls),
			signed(row.CountEffect), signed(row.PerCallEffect),
			share, row.Change, row.Path)
	}
	return tw.Flush()
}

type jsonSide struct {
	Traces     int   `json:"traces"`
	DurationNs int64 `json:"median_duration_ns"`
}

type jsonStat struct {
	Calls  float64 `json:"calls"`
	WallNs int64   `json:"wall_ns"`
	SelfNs int64   `json:"self_ns"`
}

type jsonRow struct {
	Path            string   `json:"path"`
	Change          Change   `json:"change"`
	Baseline        jsonStat `json:"baseline"`
	Candidate       jsonStat `json:"candidate"`
	SelfDeltaNs     int64    `json:"self_delta_ns"`
	WallDeltaNs     int64    `json:"wall_delta_ns"`
	CountEffectNs   int64    `json:"count_effect_ns"`
	PerCallEffectNs int64    `json:"per_call_effect_ns"`
	Share           *float64 `json:"share"` // null when the trace duration did not change
}

type jsonReport struct {
	Baseline        jsonSide  `json:"baseline"`
	Candidate       jsonSide  `json:"candidate"`
	DurationDeltaNs int64     `json:"duration_delta_ns"`
	Spans           []jsonRow `json:"spans"`
}

// WriteJSON prints the report as one indented JSON document. Durations are
// integer nanoseconds.
func WriteJSON(w io.Writer, r Report) error {
	out := jsonReport{
		Baseline:        jsonSide{r.Baseline.Traces, int64(r.Baseline.Duration)},
		Candidate:       jsonSide{r.Candidate.Traces, int64(r.Candidate.Duration)},
		DurationDeltaNs: int64(r.DurationDelta),
		Spans:           make([]jsonRow, 0, len(r.Rows)),
	}
	st := func(s Stat) jsonStat { return jsonStat{s.Calls, int64(s.Wall), int64(s.Self)} }
	for _, row := range r.Rows {
		jr := jsonRow{
			Path: row.Path, Change: row.Change,
			Baseline: st(row.Baseline), Candidate: st(row.Candidate),
			SelfDeltaNs: int64(row.SelfDelta), WallDeltaNs: int64(row.WallDelta),
			CountEffectNs: int64(row.CountEffect), PerCallEffectNs: int64(row.PerCallEffect),
		}
		if !math.IsNaN(row.Share) {
			s := row.Share
			jr.Share = &s
		}
		out.Spans = append(out.Spans, jr)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false) // keep the " > " path separator readable
	return enc.Encode(out)
}

// round trims durations to a readable precision: microseconds below a second,
// milliseconds above.
func round(d time.Duration) time.Duration {
	if abs(d) >= time.Second {
		return d.Round(time.Millisecond)
	}
	return d.Round(time.Microsecond)
}

func signed(d time.Duration) string {
	d = round(d)
	if d > 0 {
		return "+" + d.String()
	}
	return d.String()
}

func calls(c float64) string { return strconv.FormatFloat(c, 'f', -1, 64) }
