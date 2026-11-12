package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"

	"github.com/moveeeax/otel-trace-replay/internal/diff"
	"github.com/moveeeax/otel-trace-replay/internal/trace"
)

type diffFlags struct {
	baseline  []string
	candidate []string
	minDelta  time.Duration
	format    string
}

func newDiffCmd() *cobra.Command {
	f := &diffFlags{}
	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Show where the time moved between two recordings of the same operation",
		Long: "diff aligns the spans of a baseline and a candidate recording by their path in\n" +
			"the span tree (span names from the root down, not span IDs) and reports the\n" +
			"change in self time, wall time and call count per path, largest self-time\n" +
			"change first. Each side may hold many traces, across one or more files; every\n" +
			"figure is the median across that side's traces.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDiff(cmd, f)
		},
	}
	fl := cmd.Flags()
	fl.StringArrayVarP(&f.baseline, "baseline", "b", nil, "recorded OTLP/JSON trace file of the baseline; repeat for more files (required)")
	fl.StringArrayVarP(&f.candidate, "candidate", "c", nil, "recorded OTLP/JSON trace file of the candidate; repeat for more files (required)")
	fl.DurationVar(&f.minDelta, "min-delta", 0, "hide spans whose self and wall time moved less than this, unless their call count changed")
	fl.StringVar(&f.format, "format", "table", "output format: table or json")
	_ = cmd.MarkFlagRequired("baseline")
	_ = cmd.MarkFlagRequired("candidate")
	return cmd
}

func runDiff(cmd *cobra.Command, f *diffFlags) error {
	if f.format != "table" && f.format != "json" {
		return fmt.Errorf("unknown --format %q: want table or json", f.format)
	}
	if f.minDelta < 0 {
		return fmt.Errorf("--min-delta must not be negative")
	}
	base, err := loadSide(f.baseline)
	if err != nil {
		return err
	}
	cand, err := loadSide(f.candidate)
	if err != nil {
		return err
	}
	if base.Traces == 0 || cand.Traces == 0 {
		return fmt.Errorf("no spans found in the baseline or the candidate")
	}
	r := diff.Compare(base, cand, f.minDelta)
	if f.format == "json" {
		return diff.WriteJSON(cmd.OutOrStdout(), r)
	}
	return diff.WriteTable(cmd.OutOrStdout(), r)
}

func loadSide(paths []string) (diff.Side, error) {
	tds := make([]*tracepb.TracesData, 0, len(paths))
	for _, p := range paths {
		td, err := trace.Load(p)
		if err != nil {
			return diff.Side{}, err
		}
		tds = append(tds, td)
	}
	return diff.Aggregate(tds...), nil
}
