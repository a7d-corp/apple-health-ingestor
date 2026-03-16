# apple-health-ingestor

HTTP API that receives JSON payloads from the [Health Auto Export](https://www.healthautoexport.com/) iOS app and writes the data to InfluxDB v2.

## Configuration

Copy `.env.example` to `.env` and fill in the values. All variables are required except `PORT`.

| Variable | Default | Description |
|---|---|---|
| `INFLUX_URL` | — | InfluxDB URL |
| `INFLUX_TOKEN` | — | InfluxDB API token |
| `INFLUX_ORG` | — | InfluxDB org name |
| `INFLUX_BUCKET` | — | InfluxDB bucket name |
| `INGESTION_API_KEY` | — | API key for the `/ingest` endpoint |
| `PORT` | `8080` | HTTP listen port |
| `INFLUX_BATCH_SIZE` | `500` | Max points per write call |
| `INFLUX_FLUSH_INTERVAL_MS` | `1000` | Write flush interval (ms) |
| `INFLUX_MAX_RETRIES` | `3` | Write retry attempts |
| `INFLUX_RETRY_INTERVAL_MS` | `500` | Delay between retries (ms) |

## Health Auto Export setup

1. Install [Health Auto Export](https://www.healthautoexport.com/) on your iPhone.
2. Go to **Automations** → **Add Automation**.
3. Set **Export Format** to `JSON` and **Export Type** to `REST API`.
4. Set the URL to `http://<your-host>:<PORT>/ingest`.
5. Add a header: `X-API-Key: <your INGESTION_API_KEY>`.
6. Choose the metrics/workouts to export and set a sync interval.

## Running

```bash
# Build and run
go build -o health-ingestion .
./health-ingestion

# Docker
docker build -t health-ingestion .
docker run --env-file .env -p 8080:8080 health-ingestion
```

## API

| Method | Path | Auth | Description |
|---|---|---|---|
| `POST` | `/ingest` | `X-API-Key` header | Ingest Health Auto Export payload |
| `GET` | `/health` | none | Liveness check |
