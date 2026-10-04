# apple-health-ingestor

Receives health metrics from the iOS app [Health Auto Export](https://www.healthyapps.dev/)
(REST API automation) and writes them to InfluxDB 2.x.

## Endpoints

| Method | Path | Auth | Description |
|---|---|---|---|
| `POST` | `/ingest` | `Authorization: Bearer <INGEST_TOKEN>` | Ingest a Health Auto Export JSON payload |
| `GET` | `/healthz` | none | Liveness (does not check InfluxDB) |

`/ingest` responds `200 {"written":N,"skipped":M}`, `401` for a bad token, `400` for
malformed JSON, `408` when reading the body times out, `413` when the body exceeds `MAX_BODY_BYTES`, and `503` when InfluxDB
rejects the write (Health Auto Export will retry).

## Configuration

| Variable | Required | Default | Description |
|---|---|---|---|
| `INGEST_TOKEN` | yes | | Bearer token Health Auto Export must send |
| `INFLUX_URL` | yes | | InfluxDB URL, e.g. `http://influxdb:8086` |
| `INFLUX_TOKEN` | yes | | InfluxDB API token with write access to the bucket |
| `INFLUX_ORG` | yes | | InfluxDB organisation |
| `INFLUX_BUCKET` | yes | | InfluxDB bucket (must already exist) |
| `MAX_BODY_BYTES` | no | `104857600` | Maximum request body size |

The service listens on port `8080` (plain HTTP).

## Running

```bash
docker run -d --name apple-health-ingestor -p 8080:8080 \
  -e INGEST_TOKEN=... -e INFLUX_URL=http://influxdb:8086 -e INFLUX_TOKEN=... \
  -e INFLUX_ORG=home -e INFLUX_BUCKET=health \
  ghcr.io/a7d-corp/apple-health-ingestor:latest
```

`latest` is published from the first `v*` release; `:main` tracks the main branch.

### Docker Compose (ingestor + InfluxDB)

```bash
cp .env.example .env   # edit the values
docker compose up -d
```

If you already run InfluxDB, edit `docker-compose.yml`:
delete the `influxdb` service and its volumes, remove the ingestor service's `depends_on`,
and change the ingestor service's `INFLUX_URL` to your instance.

### TLS

The service does not terminate TLS. Put a reverse proxy in front of it, e.g. Caddy:

```
health.example.com {
	reverse_proxy localhost:8080
}
```

## Health Auto Export setup

Create a **REST API** automation in Health Auto Export:

- **URL:** `https://<your-host>/ingest`
- **Headers:** `Authorization` = `Bearer <INGEST_TOKEN>`
- **Data type:** Health Metrics (other data types are ignored)
- **Export format:** JSON
- **Date format:** timestamps must be in `yyyy-MM-dd HH:mm:ss Z` format (e.g. `2025-12-15 09:00:10 +0000`, as in [`testdata/sample-data.json`](testdata/sample-data.json)); entries with other formats are skipped and logged.

For historical imports, export in date-range batches rather than one huge request.
An example of what Health Auto Export sends is in [`testdata/sample-data.json`](testdata/sample-data.json).

## Data model in InfluxDB

Each entry in `data.metrics[].data[]` becomes one point:

- **Measurement:** the metric name (e.g. `heart_rate`, `sleep_analysis`)
- **Tags:** `units` and, where present, `source`, `context`, `value` (sleep stage)
- **Fields:** every numeric value (e.g. `qty`, `Min`, `Avg`, `Max`)
- **Timestamp:** the entry's `date`, second precision

Arrays such as `heartbeatSeries` and other strings are dropped. Re-sending
overlapping data is safe: InfluxDB overwrites points with the same measurement,
tags and timestamp. Entries without a valid date or without numeric values are
skipped and logged.

## Development

```bash
go test ./...                                # unit tests
go test -tags integration ./...              # integration tests (needs Docker)
```
