# apple-health-ingestor — Design

Date: 2026-10-04

## Purpose

A Go HTTP service that receives health metrics pushed by the iOS app
[Health Auto Export](https://www.healthyapps.dev/) (HAE) via its REST API
automation and writes them to InfluxDB 2.x.

- Single user, single token.
- Runs in Docker; exposed externally behind Caddy, which terminates TLS.
- InfluxDB 2.x runs separately on the same host (deployed after this code is
  written).

## Input payload

`testdata/sample-data.json` (anonymised, noise added, committed) is the
reference for what HAE sends. Shape:

```json
{
  "data": {
    "metrics": [
      {
        "name": "heart_rate",
        "units": "count/min",
        "data": [
          {"date": "2025-12-15 00:01:44 +0000", "start": "...", "end": "...",
           "source": "Apple Watch", "context": "Not Set",
           "Min": 66, "Avg": 66, "Max": 66}
        ]
      }
    ]
  }
}
```

Observed metric shapes in the sample:

| Metric | Numeric fields | String fields of interest | Other |
|---|---|---|---|
| most (e.g. `blood_oxygen_saturation`, `weight_body_mass`) | `qty` | `source` | |
| `heart_rate` | `Min`, `Avg`, `Max` | `source`, `context` | |
| `sleep_analysis` (unaggregated) | `qty` (hours) | `source`, `value` (stage: Core, Deep, …) | `startDate`, `endDate` |
| `heart_rate_variability` | `qty` | `source` | `heartbeatSeries` array |

All entries carry `date`, `start`, `end` in the format `2006-01-02 15:04:05 -0700`.

## Scope

In scope (v1): `data.metrics[]` only.

Out of scope (v1): workouts, ECG, symptoms, medications, state of mind and any
other top-level key (ignored and logged); streaming JSON parsing; multiple
users; TLS in the app; Prometheus metrics.

## HTTP API

Listens on `:8080`, plain HTTP.

### `POST /ingest`

- Requires `Authorization: Bearer <INGEST_TOKEN>`; compared in constant time.
  Missing/wrong token → `401`.
- Body limited to `MAX_BODY_BYTES` (default 100 MB) → `413` when exceeded.
- Malformed JSON → `400`.
- Success → `200` with JSON body `{"written": N, "skipped": M}`.
- InfluxDB write failure → `5xx`, so HAE retries the request.
- Response is sent only after all points are written (blocking writes,
  batches of ~5000 points).

### `GET /healthz`

No auth. Returns `200` if the process is alive. Does not check InfluxDB.

### Server timeouts

Read timeout ~5 minutes to accommodate large backfill uploads over mobile
networks. Historical imports (1–2 years) are done by the user in date-range
batches from HAE, not by streaming.

## Mapping to InfluxDB

Generic mapper — no per-metric code, so new metric types work unchanged.

For each metric `m` and each entry `e` in `m.data`:

- **Measurement**: `m.name`
- **Tags** (explicit allow-list, only when present and non-empty):
  - `units` ← `m.units`
  - `source`, `context`, `value` ← from `e`
- **Fields**: every numeric scalar in `e` (e.g. `qty`, `Min`, `Avg`, `Max`,
  aggregated sleep fields such as `deep`, `rem`, `totalSleep`), as float64.
- **Timestamp**: `e.date`, parsed with `2006-01-02 15:04:05 -0700`, second
  precision.
- **Ignored**: arrays/objects (e.g. `heartbeatSeries`), and strings not on the
  tag allow-list (`date`, `start`, `end`, `startDate`, `endDate`, …). These must
  never become tags, otherwise every point becomes its own series.

Skipped points (logged as a warning, counted in `skipped`):
- `date` missing or unparseable;
- no numeric fields.

A bad point never fails the whole request.

### Deduplication

None in the app. HAE re-sends overlapping windows; InfluxDB overwrites points
with identical measurement + tag set + timestamp, which makes re-sends
idempotent. Consequence: tags must be stable and derived only from the data.

## Configuration

Environment variables:

| Variable | Required | Default | Purpose |
|---|---|---|---|
| `INGEST_TOKEN` | yes | | Bearer token HAE must send |
| `INFLUX_URL` | yes | | e.g. `http://influxdb:8086` |
| `INFLUX_TOKEN` | yes | | InfluxDB API token with write access |
| `INFLUX_ORG` | yes | | InfluxDB org |
| `INFLUX_BUCKET` | yes | | InfluxDB bucket |
| `MAX_BODY_BYTES` | no | `104857600` | Request body limit |

The app fails to start if a required variable is missing. It does not create
the org or bucket; that is part of InfluxDB setup.

## Logging

`log/slog`, JSON to stdout. One line per ingest request (points written,
skipped, duration, status), plus warnings for skipped points / unknown
top-level keys and errors.

## Repository layout

- Go module `github.com/a7d-corp/apple-health-ingestor`, Go 1.27.
- `testdata/sample-data.json` — moved from the repo root; fixture and payload
  reference.
- `Dockerfile` — multi-stage, static binary on a distroless/scratch base.
- `docker-compose.yml` + `.env.example` — turnkey stack for other users:
  ingestor + `influxdb:2` initialised via `DOCKER_INFLUXDB_INIT_*` (org, bucket,
  admin token). Not used by the author; existing-InfluxDB users drop the
  service and set `INFLUX_URL`.
- `README.md` — setup, configuration, required HAE settings (REST API
  automation, JSON format, date format `yyyy-MM-dd HH:mm:ss Z`,
  `Authorization` header), note on putting Caddy (or another proxy) in front
  for TLS, link to the sample payload.

## Testing

- **Mapper unit tests** against `testdata/sample-data.json`: point count per
  metric, exact tags/fields/timestamp for representative entries of each
  shape (qty, heart_rate Min/Avg/Max + context, sleep stage tag, HRV with
  `heartbeatSeries` dropped), skip behaviour for bad dates/no numeric fields.
- **Handler tests** with a fake writer: auth (missing/wrong/correct token),
  body size limit, malformed JSON, writer error → 5xx, success response body.
- **Integration test** (build tag `integration`) against a real `influxdb:2`
  via testcontainers-go: ingest the sample, query back, and verify that
  re-ingesting the same payload does not duplicate points.

## CI / release (GitHub Actions)

- Every push and PR: `go vet`, `golangci-lint`, unit tests.
- Separate job: integration tests (Docker available on hosted runners).
- Push to `main`: multi-arch (amd64 + arm64) image
  `ghcr.io/a7d-corp/apple-health-ingestor:main`.
- Tag `v*`: semver image tags (`vX.Y.Z`, `X.Y`) and `latest`.
