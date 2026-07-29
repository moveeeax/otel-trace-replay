# otel-trace-replay

[![ci](https://github.com/moveeeax/otel-trace-replay/actions/workflows/ci.yml/badge.svg)](https://github.com/moveeeax/otel-trace-replay/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Replay a recorded OpenTelemetry trace into a local collector, so you can build and
test dashboards against real span data without generating live traffic.

You record one representative trace once (an OTLP/JSON file), then replay it on demand
— at real speed, sped up, in a burst, or on a loop — while you wire up panels in Tempo,
Jaeger, Grafana, or whatever sits behind your collector.

## What it does

- Loads a recorded trace in **OTLP/JSON** — the `{"resourceSpans": [...]}` document the
  collector's file exporter writes.
- Re-emits every span over **OTLP/gRPC** to a configurable collector endpoint.
- Preserves **relative span timing**, with an optional `--scale` multiplier (`--scale 0`
  fires everything at once).
- Runs a fixed number of passes (`--times`) or loops forever (`--loop`).
- Optionally rewrites embedded timestamps so the trace appears to happen **now**
  (`--rewrite-time`) — handy for dashboards with a "last 15 minutes" window.
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
| `--dry-run`      | `false`           | Parse and schedule only; do not connect or emit     |

### Local collector

A minimal collector that accepts OTLP/gRPC on `:4317` and logs received spans lives in
[`examples/collector-config.yaml`](examples/collector-config.yaml):

```
docker run --rm -p 4317:4317 \
  -v "$PWD/examples/collector-config.yaml:/etc/otelcol/config.yaml" \
  otel/opentelemetry-collector:latest --config /etc/otelcol/config.yaml
```

Point its `traces` pipeline at Tempo/Jaeger instead of `debug` to develop real dashboards.

## Install

```
go install github.com/moveeeax/otel-trace-replay@latest
```

or clone and `go build -o otel-trace-replay .`

## License

MIT — see [LICENSE](LICENSE).
