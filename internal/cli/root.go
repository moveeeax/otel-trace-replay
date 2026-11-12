// Package cli wires the otel-trace-replay command-line interface.
package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/moveeeax/otel-trace-replay/internal/otlp"
	"github.com/moveeeax/otel-trace-replay/internal/replay"
	"github.com/moveeeax/otel-trace-replay/internal/trace"
)

type flags struct {
	file        string
	endpoint    string
	scale       float64
	times       int
	loop        bool
	rewriteTime bool
	newIDs      bool
	dryRun      bool
}

// NewRootCmd builds the root command. It is a constructor so tests can execute
// it with isolated flags and captured output.
func NewRootCmd() *cobra.Command {
	f := &flags{}
	cmd := &cobra.Command{
		Use:   "otel-trace-replay",
		Short: "Replay a recorded OpenTelemetry trace into a local collector",
		Long: "otel-trace-replay loads a recorded OTLP/JSON trace and re-emits its spans\n" +
			"over OTLP/gRPC to a local collector, preserving relative span timing so you\n" +
			"can build and test dashboards without live traffic.",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return run(cmd, f)
		},
	}
	fl := cmd.Flags()
	fl.StringVarP(&f.file, "file", "f", "", "path to a recorded OTLP/JSON trace (required)")
	fl.StringVarP(&f.endpoint, "endpoint", "e", "localhost:4317", "OTLP/gRPC collector endpoint")
	fl.Float64Var(&f.scale, "scale", 1.0, "timing multiplier; 0 replays as fast as possible")
	fl.IntVarP(&f.times, "times", "n", 1, "number of replay passes")
	fl.BoolVar(&f.loop, "loop", false, "replay forever until interrupted")
	fl.BoolVar(&f.rewriteTime, "rewrite-time", false, "shift span timestamps so the trace starts now")
	fl.BoolVar(&f.newIDs, "new-ids", false, "give every pass fresh trace and span IDs (default on with --loop or --times > 1)")
	fl.BoolVar(&f.dryRun, "dry-run", false, "parse and schedule only; do not connect or emit")
	_ = cmd.MarkFlagRequired("file")
	cmd.AddCommand(newDiffCmd())
	return cmd
}

// newIDsEnabled resolves --new-ids. Unless set explicitly, IDs are regenerated
// whenever more than one pass is sent: re-sending the same IDs makes Tempo or
// Jaeger merge every pass into one trace.
func newIDsEnabled(cmd *cobra.Command, f *flags) bool {
	if cmd.Flags().Changed("new-ids") {
		return f.newIDs
	}
	return f.loop || f.times > 1
}

func run(cmd *cobra.Command, f *flags) error {
	td, err := trace.Load(f.file)
	if err != nil {
		return err
	}
	n := trace.CountSpans(td)
	if n == 0 {
		return fmt.Errorf("no spans found in %s", f.file)
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "loaded %d span(s) from %s\n", n, f.file)

	newIDs := newIDsEnabled(cmd, f)
	if f.dryRun {
		sched := replay.BuildSchedule(replay.Flatten(td), f.scale)
		var span time.Duration
		if len(sched) > 0 {
			span = sched[len(sched)-1].Delay
		}
		fmt.Fprintf(out, "dry-run: would emit %d span(s) over %s (scale %.2f)\n", len(sched), span, f.scale)
		if newIDs {
			ids, err := replay.NewIDMap(replay.Flatten(td))
			if err != nil {
				return err
			}
			fmt.Fprintln(out, "dry-run: new IDs for pass 1 (each pass draws its own):")
			for _, m := range ids.Mappings() {
				fmt.Fprintf(out, "  %-5s %s -> %s\n", m.Kind, m.Old, m.New)
			}
		}
		return nil
	}

	client, err := otlp.Dial(f.endpoint)
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Fprintf(out, "replaying to %s ...\n", f.endpoint)
	stats, err := replay.Run(ctx, td, client, replay.Options{
		Scale:       f.scale,
		Times:       f.times,
		Loop:        f.loop,
		RewriteTime: f.rewriteTime,
		NewIDs:      newIDs,
	})
	if err != nil && ctx.Err() == nil {
		return err
	}
	fmt.Fprintf(out, "done: %d span(s) across %d pass(es)\n", stats.Spans, stats.Passes)
	return nil
}
