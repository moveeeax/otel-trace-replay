# otel-trace-replay

[![ci](https://github.com/moveeeax/otel-trace-replay/actions/workflows/ci.yml/badge.svg)](https://github.com/moveeeax/otel-trace-replay/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.26%20%7C%201.27-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Replay a recorded OpenTelemetry trace into a local collector, so you can build and
test dashboards against real span data without generating live traffic.

You record one representative trace once (an OTLP/JSON file), then replay it on demand
— at real speed, sped up, in a burst, or on a loop — while you wire up panels in Tempo,
Jaeger, Grafana, or whatever sits behind your collector.

## What it does

- Loads a recorded trace in **OTLP/JSON** — the `{"resourceSpans": [...]}` document the
  collector's file exporter writes, either as one document or as JSON Lines (one document
  per line). Trace and span IDs are read as hex, as the OTLP/JSON encoding specifies.
- Re-emits every span over **OTLP/gRPC** to a configurable collector endpoint.
- Preserves **relative span timing**, with an optional `--scale` multiplier (`--scale 0`
  fires everything at once).
- Runs a fixed number of passes (`--times`) or loops forever (`--loop`).
- Optionally rewrites embedded timestamps so the trace appears to happen **now**
  (`--rewrite-time`) — handy for dashboards with a "last 15 minutes" window.
- Gives every pass fresh trace and span IDs when it sends more than one pass, so Tempo or
  Jaeger stores each pass as its own trace instead of merging them (`--new-ids`).
- Compares two recordings of the same operation and names the spans where the time moved
  (`diff` subcommand).
- Ships as a single self-contained binary with no runtime dependencies.

## How it works

The trace is parsed straight into OTLP protobuf types via `protojson`, then flattened into
a single list of spans sorted by start time. Each span's emission delay is computed relative
to the earliest span in the trace and multiplied by `--scale`; the replayer sleeps between
spans to reproduce the original cadence and re-wraps each span — with its original resource
and scope attributes intact — into an OTLP export request.

`--rewrite-time` shifts every embedded `startTime`/`endTime` (and span-event timestamps) by a
constant offset so the earliest span lands at the current wall clock, keeping all durations and
inter-span gaps exact. That decouples *when* a span is emitted (`--scale`) from *what timestamp*
it carries (`--rewrite-time`), which is what makes a months-old recording light up a "last 15
minutes" dashboard.

`--new-ids` draws, for every pass, a fresh random trace ID per recorded trace and a fresh span
ID per span, and rewrites `traceId`, `spanId`, `parentSpanId` and span links through that
mapping, so the tree structure of each pass stays intact. IDs that point outside the recording
(a parent that was not captured, a link to another trace) keep their original value. Unless you
set the flag yourself, it is on with `--loop` or `--times` above 1 and off for a single pass.

## Usage

```
$ otel-trace-replay --file examples/trace.json --dry-run
loaded 3 span(s) from examples/trace.json
dry-run: would emit 3 span(s) over 200ms (scale 1.00)

$ otel-trace-replay --file examples/trace.json --endpoint localhost:4317 --loop --rewrite-time
loaded 3 span(s) from examples/trace.json
replaying to localhost:4317 ...
```

Flags:

| Flag             | Default           | Meaning                                             |
| ---------------- | ----------------- | --------------------------------------------------- |
| `--file, -f`     | *(required)*      | Path to a recorded OTLP/JSON trace                  |
| `--endpoint, -e` | `localhost:4317`  | OTLP/gRPC collector endpoint                        |
| `--scale`        | `1.0`             | Timing multiplier; `0` replays as fast as possible  |
| `--times, -n`    | `1`               | Number of replay passes                             |
| `--loop`         | `false`           | Replay forever until interrupted                    |
| `--rewrite-time` | `false`           | Shift span timestamps so the trace starts now       |
| `--new-ids`      | on if passes > 1  | Fresh trace and span IDs on every pass              |
| `--dry-run`      | `false`           | Parse and schedule only; do not connect or emit     |

With `--new-ids` in effect, `--dry-run` also prints the old-to-new ID mapping it drew for pass 1
(every real pass draws its own).

### Comparing two recordings

`diff` turns "it got slower" into a named span. It aligns the spans of a baseline and a
candidate by their path in the span tree (span names from the root down, e.g.
`POST /checkout > charge-card`), not by span ID, and reports per path:

- the change in self time (span duration minus the time covered by its children) and in wall
  time;
- the call count on each side, so a span that appeared, disappeared or is now called five times
  instead of once (an N+1) shows up even when each call is cheap;
- the self-time change split into the part explained by the call count (`FROM CALLS`, at the
  baseline cost per call) and the part explained by the cost per call (`FROM PER-CALL`); the
  `CHANGE` column names whichever is larger, or `via children` when only the children moved;
- `SHARE`: the self-time change as a fraction of the change in trace duration. With parallel
  spans the shares can add up to more than 100%.

Rows are sorted by the self-time change, largest first. Each side may hold many traces, in one
file or across repeated `--baseline`/`--candidate` flags; spans are grouped by trace ID and every
figure is the median across that side's traces, so one unlucky request does not decide the
result. A span present in fewer than half of a side's traces has a median of zero calls there.

```
$ otel-trace-replay diff --baseline examples/trace.json --candidate examples/trace-slow.json --min-delta 25ms
baseline:  1 trace(s), median duration 850ms
candidate: 1 trace(s), median duration 1.23s (+380ms)

SELF    WALL    CALLS  FROM CALLS  FROM PER-CALL  SHARE  CHANGE        SPAN
+380ms  +380ms  1->1   0s          +380ms         100%   slower        POST /checkout > charge-card
0s      +380ms  1->1   0s          0s             0%     via children  POST /checkout
```

| Flag              | Default      | Meaning                                                    |
| ----------------- | ------------ | ---------------------------------------------------------- |
| `--baseline, -b`  | *(required)* | Baseline OTLP/JSON file; repeat for more files             |
| `--candidate, -c` | *(required)* | Candidate OTLP/JSON file; repeat for more files            |
| `--min-delta`     | `0`          | Hide spans that moved less than this, unless their call count changed |
| `--format`        | `table`      | `table` or `json` (durations in integer nanoseconds)       |

### Local collector

A minimal collector that accepts OTLP/gRPC on `:4317` and logs received spans lives in
[`examples/collector-config.yaml`](examples/collector-config.yaml):

```
docker run --rm -p 4317:4317 \
  -v "$PWD/examples/collector-config.yaml:/etc/otelcol/config.yaml" \
  otel/opentelemetry-collector:0.164.0 --config /etc/otelcol/config.yaml
```

Point its `traces` pipeline at Tempo/Jaeger instead of `debug` to develop real dashboards.

## Install

```
go install github.com/moveeeax/otel-trace-replay@latest
```

or clone and `go build -o otel-trace-replay .`

## License

MIT — see [LICENSE](LICENSE).
