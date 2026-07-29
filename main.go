// Command otel-trace-replay re-emits a recorded OpenTelemetry trace into a
// local collector so dashboards can be built against real span data with no
// live traffic.
package main

import (
	"fmt"
	"os"

	"github.com/moveeeax/otel-trace-replay/internal/cli"
)

func main() {
	if err := cli.NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
