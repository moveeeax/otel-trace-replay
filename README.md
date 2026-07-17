# otel-trace-replay

> Develop tracing dashboards against real recorded traces — no live traffic needed.

**Status:** 🚧 In development

## Overview

Replay a recorded OpenTelemetry trace into a local collector to develop and test dashboards.

## Features

- Load a recorded trace (OTLP JSON) from file
- Re-emit spans over OTLP to a configurable local collector endpoint
- Preserve relative span timing (optional time-scaling)
- Loop/replay-N modes for continuous local testing
- Small self-contained binary

## Stack

Go 1.22, OpenTelemetry OTLP exporter, `cobra`.

## Usage

```bash
otel-trace-replay --file trace.json --endpoint localhost:4317 --loop
```

## License

MIT
