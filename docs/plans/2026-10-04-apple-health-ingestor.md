# apple-health-ingestor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Go HTTP service that accepts Health Auto Export metric payloads on `POST /ingest` and writes them to InfluxDB 2.x, shipped as a multi-arch Docker image.

**Architecture:** `internal/hae` decodes the payload and maps each metric entry to a generic `Point` (measurement = metric name, allow-listed string tags, numeric fields, `date` timestamp). `internal/server` exposes `/ingest` (bearer auth, body limit) and `/healthz`, writing through a `PointWriter` interface. `internal/influx` implements that interface with the official blocking write API in batches of 5000. `cmd/apple-health-ingestor` wires config from env vars and runs the server.

**Tech Stack:** Go 1.27, stdlib `net/http` + `log/slog`, `github.com/influxdata/influxdb-client-go/v2` (v2.14.0), `github.com/testcontainers/testcontainers-go` (v0.44.0, integration tests only), Docker (distroless), GitHub Actions, ghcr.io.

**Spec:** `docs/specs/2026-10-04-apple-health-ingestor-design.md`

## Global Constraints

- Module path: `github.com/a7d-corp/apple-health-ingestor`. Image: `ghcr.io/a7d-corp/apple-health-ingestor`.
- Listen address fixed at `:8080`, plain HTTP.
- Env vars exactly: `INGEST_TOKEN`, `INFLUX_URL`, `INFLUX_TOKEN`, `INFLUX_ORG`, `INFLUX_BUCKET` (required), `MAX_BODY_BYTES` (optional, default `104857600`).
- Timestamp layout: `2006-01-02 15:04:05 -0700`, second precision.
- Tag allow-list: `units` (from metric) and `source`, `context`, `value` (from entry). No other key may ever become a tag.
- Fields: every numeric (JSON number) value in an entry, as float64. Arrays/objects/other strings ignored.
- Status codes: 401 bad/missing token, 413 body over limit, 400 malformed JSON, 503 InfluxDB write failure, 200 `{"written":N,"skipped":M}` on success, 405 wrong method (from the mux).
- Unit tests use the stdlib `testing` package only (no testify). Logging via `log/slog` JSON to stdout.
- The local machine runs a bubblewrap sandbox that breaks some commands (missing podman socket); if a shell command fails with a `bwrap:` error, re-run it with the sandbox disabled.

**User decisions (already made):**
- InfluxDB 2.x; it does not exist yet and will be deployed later.
- Ingest `data.metrics` only; other data types ignored.
- Static bearer token, single user.
- Caddy terminates TLS; the app serves plain HTTP.
- Generic mapper, one measurement per metric, drop `heartbeatSeries`.
- Rely on InfluxDB overwrite semantics for dedup.
- 5xx on InfluxDB failure; skip and log bad points, still 200.
- Multi-arch image (amd64 + arm64) built by GitHub Actions to ghcr.io.
- `/healthz` reports process liveness only.
- Author won't use compose, but the repo ships a turnkey `docker-compose.yml` (ingestor + `influxdb:2`) for others, configured by env vars.
- 100 MB configurable body limit; historical imports done in batches from HAE.
- `sample-data.json` is anonymised; commit it as `testdata/sample-data.json`.
- Tests: unit + handler tests, plus testcontainers integration test behind build tag `integration`.
- Logs only, no `/metrics`.
- CI: vet/lint/unit on every push and PR, integration as its own job; image `:main` on main, semver + `latest` on `v*` tags.

## File Structure

| Path | Responsibility |
|---|---|
| `go.mod`, `go.sum` | Module definition |
| `testdata/sample-data.json` | Real-shaped HAE payload; fixture + reference |
| `internal/hae/payload.go` | Decode HAE JSON into `Payload` |
| `internal/hae/points.go` | Map metrics to `Point`s, report skips |
| `internal/config/config.go` | Load and validate env config |
| `internal/server/server.go` | HTTP handler: `/ingest`, `/healthz` |
| `internal/influx/writer.go` | `PointWriter` backed by InfluxDB 2.x |
| `cmd/apple-health-ingestor/main.go` | Wiring, server lifecycle, `healthcheck` subcommand |
| `Dockerfile`, `.dockerignore` | Multi-arch static image |
| `docker-compose.yml`, `.env.example` | Turnkey stack for other users |
| `.golangci.yml`, `.github/workflows/ci.yaml`, `.github/workflows/release.yaml` | CI and image publishing |
| `README.md` | Usage documentation |

---

### Task 1: Module scaffold and payload decoding

**Goal:** Initialise the Go module, move the sample into `testdata/`, and decode HAE payloads into typed metrics.

**Files:**
- Create: `go.mod`
- Move: `sample-data.json` → `testdata/sample-data.json`
- Create: `internal/hae/payload.go`
- Test: `internal/hae/payload_test.go`

**Acceptance Criteria:**
- [ ] `testdata/sample-data.json` exists and `sample-data.json` no longer exists at the repo root.
- [ ] `hae.Decode` on the sample returns 9 metrics in file order with entry counts 8, 157, 6, 2401, 82, 370, 8, 190, 6 and no ignored keys.
- [ ] Keys under `data` other than `metrics` are returned sorted in `Payload.Ignored`.
- [ ] Malformed JSON returns an error that wraps the underlying reader/decoder error (`%w`).

**Verify:** `go test ./internal/hae/ -run TestDecode -v` → all `TestDecode*` PASS

**Steps:**

- [ ] **Step 1: Scaffold**

```bash
go mod init github.com/a7d-corp/apple-health-ingestor
mkdir -p testdata internal/hae
mv sample-data.json testdata/sample-data.json
```

- [ ] **Step 2: Write the failing tests** — `internal/hae/payload_test.go`

```go
package hae

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func loadSample(t *testing.T) Payload {
	t.Helper()
	f, err := os.Open("../../testdata/sample-data.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	p, err := Decode(f)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	return p
}

func TestDecodeSample(t *testing.T) {
	p := loadSample(t)

	want := []struct {
		name  string
		units string
		n     int
	}{
		{"apple_sleeping_wrist_temperature", "degC", 8},
		{"blood_oxygen_saturation", "%", 157},
		{"body_mass_index", "count", 6},
		{"heart_rate", "count/min", 2401},
		{"heart_rate_variability", "ms", 82},
		{"respiratory_rate", "count/min", 370},
		{"resting_heart_rate", "count/min", 8},
		{"sleep_analysis", "hr", 190},
		{"weight_body_mass", "kg", 6},
	}
	if len(p.Metrics) != len(want) {
		t.Fatalf("got %d metrics, want %d", len(p.Metrics), len(want))
	}
	for i, w := range want {
		m := p.Metrics[i]
		if m.Name != w.name || m.Units != w.units || len(m.Data) != w.n {
			t.Errorf("metric %d = {%q %q %d}, want {%q %q %d}", i, m.Name, m.Units, len(m.Data), w.name, w.units, w.n)
		}
	}
	if len(p.Ignored) != 0 {
		t.Errorf("Ignored = %v, want none", p.Ignored)
	}
}

func TestDecodeIgnoresOtherKeys(t *testing.T) {
	p, err := Decode(strings.NewReader(`{"data":{"workouts":[],"metrics":[],"ecg":[]}}`))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if want := []string{"ecg", "workouts"}; !reflect.DeepEqual(p.Ignored, want) {
		t.Errorf("Ignored = %v, want %v", p.Ignored, want)
	}
	if len(p.Metrics) != 0 {
		t.Errorf("Metrics = %v, want none", p.Metrics)
	}
}

func TestDecodeMalformed(t *testing.T) {
	for _, body := range []string{`{"data":`, `not json`, `{"data":{"metrics":{}}}`} {
		if _, err := Decode(strings.NewReader(body)); err == nil {
			t.Errorf("Decode(%q): expected error", body)
		}
	}
}
```

- [ ] **Step 3: Run to verify failure**

Run: `go test ./internal/hae/ -run TestDecode -v`
Expected: FAIL — `undefined: Decode` / `undefined: Payload`

- [ ] **Step 4: Implement** — `internal/hae/payload.go`

```go
// Package hae decodes Health Auto Export payloads and maps them to points.
package hae

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

// Metric is one entry of data.metrics in a Health Auto Export payload.
type Metric struct {
	Name  string           `json:"name"`
	Units string           `json:"units"`
	Data  []map[string]any `json:"data"`
}

// Payload is the part of a Health Auto Export payload that is ingested.
type Payload struct {
	Metrics []Metric
	// Ignored lists keys under "data" other than "metrics" (e.g. workouts), sorted.
	Ignored []string
}

// Decode reads a Health Auto Export JSON payload.
func Decode(r io.Reader) (Payload, error) {
	var raw struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return Payload{}, fmt.Errorf("decode payload: %w", err)
	}

	var p Payload
	for key, msg := range raw.Data {
		if key != "metrics" {
			p.Ignored = append(p.Ignored, key)
			continue
		}
		if err := json.Unmarshal(msg, &p.Metrics); err != nil {
			return Payload{}, fmt.Errorf("decode metrics: %w", err)
		}
	}
	sort.Strings(p.Ignored)
	return p, nil
}
```

- [ ] **Step 5: Run to verify pass**

Run: `go test ./internal/hae/ -run TestDecode -v`
Expected: PASS (3 tests)

- [ ] **Step 6: Commit**

```bash
git add go.mod testdata/sample-data.json internal/hae/payload.go internal/hae/payload_test.go
git commit -m "Add module scaffold and Health Auto Export payload decoding"
```

---

### Task 2: Generic metric-to-point mapper

**Goal:** Convert decoded metrics into InfluxDB-agnostic `Point`s per the spec's mapping rules, reporting skipped entries.

**Files:**
- Create: `internal/hae/points.go`
- Test: `internal/hae/points_test.go`

**Acceptance Criteria:**
- [ ] Sample maps to 3228 points and 0 skips; per-measurement counts match Task 1's entry counts.
- [ ] `weight_body_mass` @ `2025-12-15 09:00:10 +0000` → tags `{units:kg, source:Withings}`, fields `{qty:95.1}`.
- [ ] `heart_rate` @ `2025-12-15 00:01:44 +0000` → tags `{units:count/min, source:Apple Watch, context:Not Set}`, fields `{Min:66, Avg:66, Max:66}`.
- [ ] `sleep_analysis` @ `2025-12-14 23:02:12 +0000` → tags `{units:hr, source:Apple Watch, value:Core}`, fields `{qty:0.24099547902743021}`.
- [ ] `heart_rate_variability` @ `2025-12-15 00:56:19 +0000` → tags `{units:ms, source:Apple Watch}`, fields `{qty:37.418886525411729}` (no `heartbeatSeries`).
- [ ] Strings `start`, `end`, `startDate`, `endDate`, `date` never appear as tags or fields.
- [ ] Skips with reasons: `missing metric name`, `missing date`, `unparseable date`, `no numeric fields`.

**Verify:** `go test ./internal/hae/ -v` → all PASS

**Steps:**

- [ ] **Step 1: Write the failing tests** — `internal/hae/points_test.go`

```go
package hae

import (
	"reflect"
	"testing"
	"time"
)

func findPoint(t *testing.T, points []Point, measurement, date string) Point {
	t.Helper()
	ts, err := time.Parse(DateLayout, date)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range points {
		if p.Measurement == measurement && p.Time.Equal(ts) {
			return p
		}
	}
	t.Fatalf("no %s point at %s", measurement, date)
	return Point{}
}

func TestToPointsSampleCounts(t *testing.T) {
	points, skips := ToPoints(loadSample(t).Metrics)
	if len(skips) != 0 {
		t.Errorf("skips = %v, want none", skips)
	}
	if len(points) != 3228 {
		t.Errorf("got %d points, want 3228", len(points))
	}
	got := map[string]int{}
	for _, p := range points {
		got[p.Measurement]++
	}
	want := map[string]int{
		"apple_sleeping_wrist_temperature": 8,
		"blood_oxygen_saturation":          157,
		"body_mass_index":                  6,
		"heart_rate":                       2401,
		"heart_rate_variability":           82,
		"respiratory_rate":                 370,
		"resting_heart_rate":               8,
		"sleep_analysis":                   190,
		"weight_body_mass":                 6,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("counts = %v, want %v", got, want)
	}
}

func TestToPointsSampleShapes(t *testing.T) {
	points, _ := ToPoints(loadSample(t).Metrics)

	tests := []struct {
		measurement string
		date        string
		tags        map[string]string
		fields      map[string]float64
	}{
		{
			"weight_body_mass", "2025-12-15 09:00:10 +0000",
			map[string]string{"units": "kg", "source": "Withings"},
			map[string]float64{"qty": 95.1},
		},
		{
			"heart_rate", "2025-12-15 00:01:44 +0000",
			map[string]string{"units": "count/min", "source": "Apple Watch", "context": "Not Set"},
			map[string]float64{"Min": 66, "Avg": 66, "Max": 66},
		},
		{
			"sleep_analysis", "2025-12-14 23:02:12 +0000",
			map[string]string{"units": "hr", "source": "Apple Watch", "value": "Core"},
			map[string]float64{"qty": 0.24099547902743021},
		},
		{
			"heart_rate_variability", "2025-12-15 00:56:19 +0000",
			map[string]string{"units": "ms", "source": "Apple Watch"},
			map[string]float64{"qty": 37.418886525411729},
		},
	}
	for _, tt := range tests {
		t.Run(tt.measurement, func(t *testing.T) {
			p := findPoint(t, points, tt.measurement, tt.date)
			if !reflect.DeepEqual(p.Tags, tt.tags) {
				t.Errorf("tags = %v, want %v", p.Tags, tt.tags)
			}
			if !reflect.DeepEqual(p.Fields, tt.fields) {
				t.Errorf("fields = %v, want %v", p.Fields, tt.fields)
			}
		})
	}
}

func TestToPointsIgnoresNonAllowListedStrings(t *testing.T) {
	points, _ := ToPoints([]Metric{{
		Name:  "sleep_analysis",
		Units: "hr",
		Data: []map[string]any{{
			"date": "2025-12-14 23:02:12 +0000", "start": "x", "end": "x",
			"startDate": "x", "endDate": "x", "source": "Apple Watch",
			"value": "Core", "qty": 1.5, "heartbeatSeries": []any{map[string]any{"date": 1.0}},
		}},
	}})
	want := Point{
		Measurement: "sleep_analysis",
		Tags:        map[string]string{"units": "hr", "source": "Apple Watch", "value": "Core"},
		Fields:      map[string]float64{"qty": 1.5},
		Time:        time.Date(2025, 12, 14, 23, 2, 12, 0, time.UTC),
	}
	if len(points) != 1 || !reflect.DeepEqual(points[0].Tags, want.Tags) ||
		!reflect.DeepEqual(points[0].Fields, want.Fields) || !points[0].Time.Equal(want.Time) {
		t.Errorf("points = %+v, want [%+v]", points, want)
	}
}

func TestToPointsSkips(t *testing.T) {
	metrics := []Metric{
		{Name: "", Data: []map[string]any{{"date": "2025-12-15 09:00:10 +0000", "qty": 1.0}}},
		{Name: "m", Data: []map[string]any{
			{"qty": 1.0},
			{"date": "yesterday", "qty": 1.0},
			{"date": "2025-12-15 09:00:10 +0000", "source": "Apple Watch"},
			{"date": "2025-12-15 09:00:10 +0000", "qty": 2.0},
		}},
	}
	points, skips := ToPoints(metrics)
	if len(points) != 1 {
		t.Errorf("got %d points, want 1", len(points))
	}
	want := []Skip{
		{Metric: "", Index: 0, Reason: "missing metric name"},
		{Metric: "m", Index: 0, Reason: "missing date"},
		{Metric: "m", Index: 1, Reason: "unparseable date"},
		{Metric: "m", Index: 2, Reason: "no numeric fields"},
	}
	if !reflect.DeepEqual(skips, want) {
		t.Errorf("skips = %+v, want %+v", skips, want)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/hae/ -run TestToPoints -v`
Expected: FAIL — `undefined: ToPoints` / `undefined: Point`

- [ ] **Step 3: Implement** — `internal/hae/points.go`

```go
package hae

import "time"

// DateLayout is the timestamp format used by Health Auto Export.
const DateLayout = "2006-01-02 15:04:05 -0700"

// entryTags are the only entry keys that become tags. Any other string is
// ignored: per-point strings as tags would create one series per point.
var entryTags = []string{"source", "context", "value"}

// Point is a single time-series point derived from a metric entry.
type Point struct {
	Measurement string
	Tags        map[string]string
	Fields      map[string]float64
	Time        time.Time
}

// Skip records a metric entry that could not be mapped to a point.
type Skip struct {
	Metric string
	Index  int
	Reason string
}

// ToPoints maps metrics to points. Entries that cannot be mapped are returned
// as skips rather than failing the whole batch.
func ToPoints(metrics []Metric) ([]Point, []Skip) {
	var points []Point
	var skips []Skip
	for _, m := range metrics {
		for i, e := range m.Data {
			p, reason := toPoint(m, e)
			if reason != "" {
				skips = append(skips, Skip{Metric: m.Name, Index: i, Reason: reason})
				continue
			}
			points = append(points, p)
		}
	}
	return points, skips
}

func toPoint(m Metric, e map[string]any) (Point, string) {
	if m.Name == "" {
		return Point{}, "missing metric name"
	}
	date, _ := e["date"].(string)
	if date == "" {
		return Point{}, "missing date"
	}
	ts, err := time.Parse(DateLayout, date)
	if err != nil {
		return Point{}, "unparseable date"
	}

	fields := map[string]float64{}
	for k, v := range e {
		if f, ok := v.(float64); ok {
			fields[k] = f
		}
	}
	if len(fields) == 0 {
		return Point{}, "no numeric fields"
	}

	tags := map[string]string{}
	if m.Units != "" {
		tags["units"] = m.Units
	}
	for _, k := range entryTags {
		if s, ok := e[k].(string); ok && s != "" {
			tags[k] = s
		}
	}
	return Point{Measurement: m.Name, Tags: tags, Fields: fields, Time: ts}, ""
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/hae/ -v`
Expected: PASS (all Decode and ToPoints tests)

- [ ] **Step 5: Commit**

```bash
git add internal/hae/points.go internal/hae/points_test.go
git commit -m "Add generic metric to point mapper"
```

---

### Task 3: Environment configuration

**Goal:** Load and validate configuration from environment variables.

**Files:**
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Acceptance Criteria:**
- [ ] All five required vars set → `Config` populated, `MaxBodyBytes == 104857600`.
- [ ] Missing required vars → error `missing required environment variables: A, B` listing every missing name in declaration order.
- [ ] `MAX_BODY_BYTES=1024` → `MaxBodyBytes == 1024`; `abc`, `0`, `-5` → error.

**Verify:** `go test ./internal/config/ -v` → all PASS

**Steps:**

- [ ] **Step 1: Write the failing tests** — `internal/config/config_test.go`

```go
package config

import "testing"

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func valid() map[string]string {
	return map[string]string{
		"INGEST_TOKEN":  "secret",
		"INFLUX_URL":    "http://influxdb:8086",
		"INFLUX_TOKEN":  "influx-token",
		"INFLUX_ORG":    "home",
		"INFLUX_BUCKET": "health",
	}
}

func TestLoadValid(t *testing.T) {
	c, err := Load(env(valid()))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Config{
		IngestToken:  "secret",
		InfluxURL:    "http://influxdb:8086",
		InfluxToken:  "influx-token",
		InfluxOrg:    "home",
		InfluxBucket: "health",
		MaxBodyBytes: 104857600,
	}
	if c != want {
		t.Errorf("got %+v, want %+v", c, want)
	}
}

func TestLoadMissing(t *testing.T) {
	m := valid()
	delete(m, "INGEST_TOKEN")
	delete(m, "INFLUX_BUCKET")
	_, err := Load(env(m))
	want := "missing required environment variables: INGEST_TOKEN, INFLUX_BUCKET"
	if err == nil || err.Error() != want {
		t.Errorf("err = %v, want %q", err, want)
	}
}

func TestLoadMaxBodyBytes(t *testing.T) {
	m := valid()
	m["MAX_BODY_BYTES"] = "1024"
	c, err := Load(env(m))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.MaxBodyBytes != 1024 {
		t.Errorf("MaxBodyBytes = %d, want 1024", c.MaxBodyBytes)
	}

	for _, bad := range []string{"abc", "0", "-5"} {
		m["MAX_BODY_BYTES"] = bad
		if _, err := Load(env(m)); err == nil {
			t.Errorf("MAX_BODY_BYTES=%q: expected error", bad)
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/config/ -v`
Expected: FAIL — `undefined: Load` / `undefined: Config`

- [ ] **Step 3: Implement** — `internal/config/config.go`

```go
// Package config loads the service configuration from environment variables.
package config

import (
	"fmt"
	"strconv"
	"strings"
)

// DefaultMaxBodyBytes is the default request body limit (100 MiB).
const DefaultMaxBodyBytes = 100 << 20

// Config is the service configuration.
type Config struct {
	IngestToken  string
	InfluxURL    string
	InfluxToken  string
	InfluxOrg    string
	InfluxBucket string
	MaxBodyBytes int64
}

// Load reads the configuration using getenv (normally os.Getenv).
func Load(getenv func(string) string) (Config, error) {
	c := Config{
		IngestToken:  getenv("INGEST_TOKEN"),
		InfluxURL:    getenv("INFLUX_URL"),
		InfluxToken:  getenv("INFLUX_TOKEN"),
		InfluxOrg:    getenv("INFLUX_ORG"),
		InfluxBucket: getenv("INFLUX_BUCKET"),
		MaxBodyBytes: DefaultMaxBodyBytes,
	}

	var missing []string
	for _, v := range []struct{ name, value string }{
		{"INGEST_TOKEN", c.IngestToken},
		{"INFLUX_URL", c.InfluxURL},
		{"INFLUX_TOKEN", c.InfluxToken},
		{"INFLUX_ORG", c.InfluxOrg},
		{"INFLUX_BUCKET", c.InfluxBucket},
	} {
		if v.value == "" {
			missing = append(missing, v.name)
		}
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}

	if s := getenv("MAX_BODY_BYTES"); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n <= 0 {
			return Config{}, fmt.Errorf("MAX_BODY_BYTES must be a positive integer, got %q", s)
		}
		c.MaxBodyBytes = n
	}
	return c, nil
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/config/ -v`
Expected: PASS (3 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/config/
git commit -m "Add environment configuration loading"
```

---

### Task 4: HTTP server

**Goal:** Serve `POST /ingest` (auth, body limit, decode, map, write) and `GET /healthz`.

**Files:**
- Create: `internal/server/server.go`
- Test: `internal/server/server_test.go`

**Acceptance Criteria:**
- [ ] `GET /healthz` → 200 without auth.
- [ ] `POST /ingest` missing or wrong `Authorization` → 401, writer not called.
- [ ] `GET /ingest` → 405.
- [ ] Malformed JSON → 400; body larger than the limit → 413; writer error → 503.
- [ ] Sample payload with correct token → 200, `Content-Type: application/json`, body `{"written":3228,"skipped":0}`, writer received 3228 points.
- [ ] Payload with one undated entry → 200 with `"skipped":1`.

**Verify:** `go test ./internal/server/ -v` → all PASS

**Steps:**

- [ ] **Step 1: Write the failing tests** — `internal/server/server_test.go`

```go
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/a7d-corp/apple-health-ingestor/internal/hae"
)

const token = "secret"

type fakeWriter struct {
	calls  int
	points []hae.Point
	err    error
}

func (f *fakeWriter) Write(_ context.Context, points []hae.Point) error {
	f.calls++
	f.points = append(f.points, points...)
	return f.err
}

func do(t *testing.T, h http.Handler, method, path, auth string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func newHandler(w PointWriter, maxBody int64) http.Handler {
	return New(token, maxBody, w, slog.New(slog.DiscardHandler))
}

func TestHealthz(t *testing.T) {
	rec := do(t, newHandler(&fakeWriter{}, 1<<20), http.MethodGet, "/healthz", "", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestIngestRejects(t *testing.T) {
	valid := `{"data":{"metrics":[]}}`
	tests := []struct {
		name    string
		method  string
		auth    string
		body    string
		maxBody int64
		want    int
	}{
		{"no auth", http.MethodPost, "", valid, 1 << 20, http.StatusUnauthorized},
		{"wrong token", http.MethodPost, "Bearer nope", valid, 1 << 20, http.StatusUnauthorized},
		{"no bearer prefix", http.MethodPost, token, valid, 1 << 20, http.StatusUnauthorized},
		{"wrong method", http.MethodGet, "Bearer " + token, "", 1 << 20, http.StatusMethodNotAllowed},
		{"malformed", http.MethodPost, "Bearer " + token, `{"data":`, 1 << 20, http.StatusBadRequest},
		{"too large", http.MethodPost, "Bearer " + token, valid, 10, http.StatusRequestEntityTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &fakeWriter{}
			rec := do(t, newHandler(w, tt.maxBody), tt.method, "/ingest", tt.auth, strings.NewReader(tt.body))
			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d", rec.Code, tt.want)
			}
			if w.calls != 0 {
				t.Errorf("writer called %d times, want 0", w.calls)
			}
		})
	}
}

func TestIngestWriterError(t *testing.T) {
	w := &fakeWriter{err: errors.New("influx down")}
	body := `{"data":{"metrics":[{"name":"m","units":"u","data":[{"date":"2025-12-15 09:00:10 +0000","qty":1}]}]}}`
	rec := do(t, newHandler(w, 1<<20), http.MethodPost, "/ingest", "Bearer "+token, strings.NewReader(body))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestIngestSample(t *testing.T) {
	f, err := os.Open("../../testdata/sample-data.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	w := &fakeWriter{}
	rec := do(t, newHandler(w, 100<<20), http.MethodPost, "/ingest", "Bearer "+token, f)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	var got map[string]int
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got["written"] != 3228 || got["skipped"] != 0 {
		t.Errorf("response = %v, want written=3228 skipped=0", got)
	}
	if len(w.points) != 3228 {
		t.Errorf("writer got %d points, want 3228", len(w.points))
	}
}

func TestIngestCountsSkips(t *testing.T) {
	w := &fakeWriter{}
	body := `{"data":{"metrics":[{"name":"m","units":"u","data":[{"qty":1},{"date":"2025-12-15 09:00:10 +0000","qty":2}]}]}}`
	rec := do(t, newHandler(w, 1<<20), http.MethodPost, "/ingest", "Bearer "+token, strings.NewReader(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if want := `{"skipped":1,"written":1}`; strings.TrimSpace(rec.Body.String()) != want {
		t.Errorf("body = %s, want %s", rec.Body, want)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/server/ -v`
Expected: FAIL — `undefined: New` / `undefined: PointWriter`

- [ ] **Step 3: Implement** — `internal/server/server.go`

```go
// Package server implements the HTTP API of the ingestor.
package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/a7d-corp/apple-health-ingestor/internal/hae"
)

// PointWriter persists points.
type PointWriter interface {
	Write(ctx context.Context, points []hae.Point) error
}

// New returns the handler serving POST /ingest and GET /healthz.
func New(token string, maxBodyBytes int64, writer PointWriter, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("POST /ingest", &ingestHandler{
		want:         []byte("Bearer " + token),
		maxBodyBytes: maxBodyBytes,
		writer:       writer,
		log:          log,
	})
	return mux
}

type ingestHandler struct {
	want         []byte
	maxBodyBytes int64
	writer       PointWriter
	log          *slog.Logger
}

func (h *ingestHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), h.want) != 1 {
		h.log.Warn("unauthorized ingest request", "remote", r.RemoteAddr)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	payload, err := hae.Decode(http.MaxBytesReader(w, r.Body, h.maxBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			h.log.Warn("request body too large", "limit", h.maxBodyBytes)
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		h.log.Warn("malformed payload", "error", err)
		http.Error(w, "malformed payload", http.StatusBadRequest)
		return
	}

	for _, key := range payload.Ignored {
		h.log.Info("ignoring unsupported data type", "key", key)
	}
	points, skips := hae.ToPoints(payload.Metrics)
	for _, s := range skips {
		h.log.Warn("skipping entry", "metric", s.Metric, "index", s.Index, "reason", s.Reason)
	}

	if err := h.writer.Write(r.Context(), points); err != nil {
		h.log.Error("writing points", "error", err, "points", len(points))
		http.Error(w, "failed to write points", http.StatusServiceUnavailable)
		return
	}

	h.log.Info("ingested payload", "written", len(points), "skipped", len(skips), "duration", time.Since(start))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int{"written": len(points), "skipped": len(skips)})
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/server/ -v`
Expected: PASS (all tests)

- [ ] **Step 5: Commit**

```bash
git add internal/server/
git commit -m "Add HTTP server with ingest and healthz endpoints"
```

---

### Task 5: InfluxDB writer and integration test

**Goal:** Implement `PointWriter` against InfluxDB 2.x with blocking batched writes, proven by an integration test that also checks re-ingest does not duplicate.

**Files:**
- Create: `internal/influx/writer.go`
- Test: `internal/influx/writer_integration_test.go` (build tag `integration`)
- Modify: `go.mod`, `go.sum`

**Acceptance Criteria:**
- [ ] `go build ./...` and `go vet ./...` succeed; plain `go test ./...` does not run the integration test.
- [ ] Integration test starts `influxdb:2.7`, onboards via the client `Setup` API, writes the sample twice.
- [ ] After each write: count of `heart_rate`/`Avg` points == 2400 (2401 entries, one exact duplicate collapses) and count of `qty` points == 827.
- [ ] `weight_body_mass` `qty` at `2025-12-15T09:00:10Z` reads back as 95.1.

**Verify:** `go test -tags integration ./internal/influx/ -v` → PASS (requires a Docker/Podman socket; with Podman set `DOCKER_HOST=unix://$XDG_RUNTIME_DIR/podman/podman.sock` and `TESTCONTAINERS_RYUK_DISABLED=true`)

**Steps:**

- [ ] **Step 1: Add dependencies**

```bash
go get github.com/influxdata/influxdb-client-go/v2@v2.14.0
go get github.com/testcontainers/testcontainers-go@v0.44.0
```

- [ ] **Step 2: Write the failing integration test** — `internal/influx/writer_integration_test.go`

```go
//go:build integration

package influx_test

import (
	"fmt"
	"os"
	"testing"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/a7d-corp/apple-health-ingestor/internal/hae"
	"github.com/a7d-corp/apple-health-ingestor/internal/influx"
)

const (
	testOrg    = "test"
	testBucket = "health"
)

// startInflux runs an InfluxDB 2 container, onboards it and returns its URL and an admin token.
func startInflux(t *testing.T) (string, string) {
	t.Helper()
	ctx := t.Context()

	c, err := testcontainers.Run(ctx, "influxdb:2.7",
		testcontainers.WithExposedPorts("8086/tcp"),
		testcontainers.WithWaitStrategy(wait.ForHTTP("/health").WithPort("8086/tcp")),
	)
	testcontainers.CleanupContainer(t, c)
	if err != nil {
		t.Fatalf("start influxdb: %v", err)
	}
	url, err := c.PortEndpoint(ctx, "8086/tcp", "http")
	if err != nil {
		t.Fatal(err)
	}

	client := influxdb2.NewClient(url, "")
	defer client.Close()
	resp, err := client.Setup(ctx, "admin", "password123", testOrg, testBucket, 0)
	if err != nil {
		t.Fatalf("onboard influxdb: %v", err)
	}
	return url, *resp.Auth.Token
}

func query(t *testing.T, client influxdb2.Client, flux string) any {
	t.Helper()
	res, err := client.QueryAPI(testOrg).Query(t.Context(), flux)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer res.Close()
	if !res.Next() {
		t.Fatalf("query returned no rows: %v", res.Err())
	}
	return res.Record().Value()
}

func count(t *testing.T, client influxdb2.Client, filter string) int64 {
	t.Helper()
	flux := fmt.Sprintf(`from(bucket: %q) |> range(start: 2025-01-01T00:00:00Z) |> filter(fn: (r) => %s) |> group() |> count()`, testBucket, filter)
	return query(t, client, flux).(int64)
}

func TestWriterIntegration(t *testing.T) {
	url, token := startInflux(t)

	f, err := os.Open("../../testdata/sample-data.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	payload, err := hae.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	points, _ := hae.ToPoints(payload.Metrics)

	w := influx.NewWriter(url, token, testOrg, testBucket)
	t.Cleanup(w.Close)
	client := influxdb2.NewClient(url, token)
	t.Cleanup(client.Close)

	// Writing twice must not duplicate: InfluxDB overwrites identical series+timestamp.
	for i := range 2 {
		if err := w.Write(t.Context(), points); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
		if got := count(t, client, `r._measurement == "heart_rate" and r._field == "Avg"`); got != 2400 {
			t.Errorf("write %d: heart_rate Avg count = %d, want 2400", i, got)
		}
		if got := count(t, client, `r._field == "qty"`); got != 827 {
			t.Errorf("write %d: qty count = %d, want 827", i, got)
		}
	}

	flux := fmt.Sprintf(`from(bucket: %q) |> range(start: 2025-12-15T09:00:10Z, stop: 2025-12-15T09:00:11Z) |> filter(fn: (r) => r._measurement == "weight_body_mass" and r._field == "qty")`, testBucket)
	if got := query(t, client, flux).(float64); got != 95.1 {
		t.Errorf("weight = %v, want 95.1", got)
	}
}
```

- [ ] **Step 3: Run to verify failure**

Run: `go test -tags integration ./internal/influx/ -v`
Expected: FAIL to compile — `undefined: influx.NewWriter`

- [ ] **Step 4: Implement** — `internal/influx/writer.go`

```go
// Package influx writes points to InfluxDB 2.x.
package influx

import (
	"context"
	"fmt"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
	"github.com/influxdata/influxdb-client-go/v2/api/write"

	"github.com/a7d-corp/apple-health-ingestor/internal/hae"
)

const batchSize = 5000

// Writer writes points to an InfluxDB 2.x bucket using blocking writes.
type Writer struct {
	client influxdb2.Client
	api    api.WriteAPIBlocking
}

// NewWriter returns a Writer for the given bucket. It does not contact the server.
func NewWriter(url, token, org, bucket string) *Writer {
	client := influxdb2.NewClientWithOptions(url, token, influxdb2.DefaultOptions().SetPrecision(time.Second))
	return &Writer{client: client, api: client.WriteAPIBlocking(org, bucket)}
}

// Write writes points in batches, returning on the first failed batch.
func (w *Writer) Write(ctx context.Context, points []hae.Point) error {
	for start := 0; start < len(points); start += batchSize {
		end := min(start+batchSize, len(points))
		batch := make([]*write.Point, 0, end-start)
		for _, p := range points[start:end] {
			fields := make(map[string]any, len(p.Fields))
			for k, v := range p.Fields {
				fields[k] = v
			}
			batch = append(batch, influxdb2.NewPoint(p.Measurement, p.Tags, fields, p.Time))
		}
		if err := w.api.WritePoint(ctx, batch...); err != nil {
			return fmt.Errorf("write points %d-%d: %w", start, end, err)
		}
	}
	return nil
}

// Close releases the client's resources.
func (w *Writer) Close() {
	w.client.Close()
}
```

- [ ] **Step 5: Tidy, then run to verify pass**

```bash
go mod tidy
go vet ./... && go test ./...
go test -tags integration ./internal/influx/ -v
```
Expected: unit tests PASS; integration `TestWriterIntegration` PASS. If no container socket is reachable locally, record that the integration test was not run locally and rely on the CI job from Task 8 — do not mark the "count == 2400 / 827" criteria verified without a real run.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/influx/
git commit -m "Add InfluxDB writer with integration test"
```

---

### Task 6: Main entrypoint

**Goal:** Wire config, writer and server into a binary with graceful shutdown and a `healthcheck` subcommand for distroless images.

**Files:**
- Create: `cmd/apple-health-ingestor/main.go`

**Acceptance Criteria:**
- [ ] Binary exits non-zero and logs the missing-variable error as JSON when required env is unset.
- [ ] With env set (InfluxDB unreachable): `GET /healthz` → 200; `POST /ingest` without token → 401; with token and the sample → 503.
- [ ] `apple-health-ingestor healthcheck` exits 0 while the server runs, 1 when it doesn't.
- [ ] SIGTERM triggers graceful shutdown (log line `shutting down`, exit 0).

**Verify:** smoke script in Step 2 prints `200`, `401`, `503`, `0`, `1`.

**Steps:**

- [ ] **Step 1: Implement** — `cmd/apple-health-ingestor/main.go`

```go
// Command apple-health-ingestor receives Health Auto Export payloads and writes them to InfluxDB.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/a7d-corp/apple-health-ingestor/internal/config"
	"github.com/a7d-corp/apple-health-ingestor/internal/influx"
	"github.com/a7d-corp/apple-health-ingestor/internal/server"
)

const addr = ":8080"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}

	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}

	writer := influx.NewWriter(cfg.InfluxURL, cfg.InfluxToken, cfg.InfluxOrg, cfg.InfluxBucket)
	defer writer.Close()

	srv := &http.Server{
		Addr:              addr,
		Handler:           server.New(cfg.IngestToken, cfg.MaxBodyBytes, writer, log),
		ReadHeaderTimeout: 10 * time.Second,
		// Generous timeouts for large backfill uploads over mobile networks.
		ReadTimeout:  5 * time.Minute,
		WriteTimeout: 5 * time.Minute,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	log.Info("listening", "addr", addr)

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// healthcheck probes the local server; used as the Docker HEALTHCHECK in distroless images.
func healthcheck() int {
	resp, err := http.Get("http://127.0.0.1" + addr + "/healthz")
	if err != nil {
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
```

- [ ] **Step 2: Build and smoke test**

```bash
go build -o /tmp/ahi ./cmd/apple-health-ingestor
/tmp/ahi; echo "exit=$?"   # expect JSON error about missing variables, exit=1

INGEST_TOKEN=secret INFLUX_URL=http://127.0.0.1:1 INFLUX_TOKEN=x INFLUX_ORG=o INFLUX_BUCKET=b /tmp/ahi &
PID=$!; sleep 1
curl -s -o /dev/null -w '%{http_code}\n' localhost:8080/healthz                                  # 200
curl -s -o /dev/null -w '%{http_code}\n' -X POST localhost:8080/ingest                           # 401
curl -s -o /dev/null -w '%{http_code}\n' -X POST -H 'Authorization: Bearer secret' \
  --data-binary @testdata/sample-data.json localhost:8080/ingest                                 # 503
/tmp/ahi healthcheck; echo $?                                                                    # 0
kill -TERM $PID; wait $PID; echo "exit=$?"                                                       # exit=0, "shutting down" logged
/tmp/ahi healthcheck; echo $?                                                                    # 1
```

(Use the scratchpad directory instead of `/tmp` if running as an agent.)

- [ ] **Step 3: Commit**

```bash
git add cmd/
git commit -m "Add main entrypoint with graceful shutdown and healthcheck"
```

---

### Task 7: Docker image and compose stack

**Goal:** Multi-arch-capable distroless image plus a turnkey compose stack for other users.

**Files:**
- Create: `Dockerfile`, `.dockerignore`, `docker-compose.yml`, `.env.example`

**Acceptance Criteria:**
- [ ] `docker build` (or `podman build`) succeeds; the final image is based on `gcr.io/distroless/static-debian12:nonroot`, runs as non-root, and exposes 8080.
- [ ] The container started with the Task 6 env vars answers `GET /healthz` with 200, and `healthcheck` inside it exits 0.
- [ ] `docker compose config` (or `podman compose config`) with `.env.example` copied to `.env` renders without errors; the ingestor's `INFLUX_URL` is `http://influxdb:8086`.

**Verify:** commands in Step 2 → build OK, `200`, exit `0`, compose config renders.

**Steps:**

- [ ] **Step 1: Write files**

`Dockerfile`:

```dockerfile
# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.27 AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/apple-health-ingestor ./cmd/apple-health-ingestor

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/apple-health-ingestor /apple-health-ingestor
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s CMD ["/apple-health-ingestor", "healthcheck"]
ENTRYPOINT ["/apple-health-ingestor"]
```

`.dockerignore`:

```
.git
.github
docs
testdata
.env
```

`docker-compose.yml`:

```yaml
# Turnkey stack: the ingestor plus a fresh InfluxDB 2.x.
# If you already run InfluxDB, delete the influxdb service and point INFLUX_URL at it.
# Put a TLS-terminating reverse proxy (e.g. Caddy) in front of port 8080.
services:
  influxdb:
    image: influxdb:2
    restart: unless-stopped
    environment:
      DOCKER_INFLUXDB_INIT_MODE: setup
      DOCKER_INFLUXDB_INIT_USERNAME: ${INFLUXDB_ADMIN_USER}
      DOCKER_INFLUXDB_INIT_PASSWORD: ${INFLUXDB_ADMIN_PASSWORD}
      DOCKER_INFLUXDB_INIT_ORG: ${INFLUX_ORG}
      DOCKER_INFLUXDB_INIT_BUCKET: ${INFLUX_BUCKET}
      DOCKER_INFLUXDB_INIT_ADMIN_TOKEN: ${INFLUX_TOKEN}
    volumes:
      - influxdb-data:/var/lib/influxdb2
      - influxdb-config:/etc/influxdb2

  ingestor:
    image: ghcr.io/a7d-corp/apple-health-ingestor:latest
    restart: unless-stopped
    depends_on:
      - influxdb
    environment:
      INGEST_TOKEN: ${INGEST_TOKEN}
      INFLUX_URL: http://influxdb:8086
      INFLUX_TOKEN: ${INFLUX_TOKEN}
      INFLUX_ORG: ${INFLUX_ORG}
      INFLUX_BUCKET: ${INFLUX_BUCKET}
    ports:
      - "8080:8080"

volumes:
  influxdb-data:
  influxdb-config:
```

`.env.example`:

```
# Token Health Auto Export sends as "Authorization: Bearer <INGEST_TOKEN>".
INGEST_TOKEN=change-me

# InfluxDB initial setup (used by the influxdb service on first start only).
INFLUXDB_ADMIN_USER=admin
INFLUXDB_ADMIN_PASSWORD=change-me-too
INFLUX_ORG=home
INFLUX_BUCKET=health
INFLUX_TOKEN=change-me-influx-token
```

- [ ] **Step 2: Build and verify** (use `podman` in place of `docker` if no Docker daemon is running)

```bash
docker build -t apple-health-ingestor:dev .
docker run -d --name ahi -p 8080:8080 \
  -e INGEST_TOKEN=secret -e INFLUX_URL=http://127.0.0.1:1 -e INFLUX_TOKEN=x \
  -e INFLUX_ORG=o -e INFLUX_BUCKET=b apple-health-ingestor:dev
sleep 1
curl -s -o /dev/null -w '%{http_code}\n' localhost:8080/healthz   # 200
docker exec ahi /apple-health-ingestor healthcheck; echo $?      # 0
docker rm -f ahi

cp .env.example .env && docker compose config && rm .env          # renders; INFLUX_URL: http://influxdb:8086
```

- [ ] **Step 3: Commit**

```bash
git add Dockerfile .dockerignore docker-compose.yml .env.example
git commit -m "Add Dockerfile and example compose stack"
```

---

### Task 8: CI and image release workflows

**Goal:** GitHub Actions for lint/unit/integration tests and multi-arch image publishing to ghcr.io.

**Files:**
- Create: `.golangci.yml`, `.github/workflows/ci.yaml`, `.github/workflows/release.yaml`

**Acceptance Criteria:**
- [ ] `ci.yaml` runs on every push and pull request: job `test` (`go vet`, golangci-lint, `go test ./...`) and job `integration` (`go test -tags integration ./...`).
- [ ] `release.yaml` runs on push to `main` and tags `v*`; builds `linux/amd64,linux/arm64`; pushes `ghcr.io/a7d-corp/apple-health-ingestor` with tags `main` (branch), `vX.Y.Z`, `X.Y` and `latest` (semver tags).
- [ ] `golangci-lint run` passes locally if installed; `actionlint` passes if installed (otherwise note it was not run).
- [ ] Action major versions are the current latest at implementation time (check each action's GitHub releases; bump the versions below if newer majors exist).

**Verify:** `golangci-lint run && actionlint` → no findings (or report which tool was unavailable)

**Steps:**

- [ ] **Step 1: Write files**

`.golangci.yml`:

```yaml
version: "2"
linters:
  exclusions:
    presets:
      # Don't flag unchecked errors from Close() and similar std calls.
      - std-error-handling
```

`.github/workflows/ci.yaml`:

```yaml
name: CI

on:
  push:
  pull_request:

permissions:
  contents: read

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: go.mod
      - run: go vet ./...
      - uses: golangci/golangci-lint-action@v8
      - run: go test ./...

  integration:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: go.mod
      - run: go test -tags integration -v ./...
```

`.github/workflows/release.yaml`:

```yaml
name: Release

on:
  push:
    branches: [main]
    tags: ["v*"]

permissions:
  contents: read
  packages: write

jobs:
  image:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: docker/setup-buildx-action@v3
      - uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}
      - id: meta
        uses: docker/metadata-action@v5
        with:
          images: ghcr.io/${{ github.repository }}
          tags: |
            type=ref,event=branch
            type=semver,pattern=v{{version}}
            type=semver,pattern={{major}}.{{minor}}
      - uses: docker/build-push-action@v6
        with:
          context: .
          platforms: linux/amd64,linux/arm64
          push: true
          tags: ${{ steps.meta.outputs.tags }}
          labels: ${{ steps.meta.outputs.labels }}
```

(No QEMU needed: the Dockerfile cross-compiles on `$BUILDPLATFORM` and the final stage only copies the binary. `metadata-action`'s default `latest=auto` flavor adds `latest` on semver tags.)

- [ ] **Step 2: Verify**

```bash
golangci-lint run
actionlint
```

- [ ] **Step 3: Commit**

```bash
git add .golangci.yml .github/
git commit -m "Add CI and image release workflows"
```

---

### Task 9: README

**Goal:** Document what the service does, how to run it, how to configure Health Auto Export, and the data model.

**Files:**
- Modify: `README.md` (currently just `# template-repo`)

**Acceptance Criteria:**
- [ ] README covers: purpose; env var table (same names/defaults as Global Constraints); `docker run` example; compose quick start (`cp .env.example .env`, `docker compose up -d`); reverse proxy/TLS note with a minimal Caddyfile; Health Auto Export setup (REST API automation, URL `https://<host>/ingest`, JSON format, `Authorization: Bearer <INGEST_TOKEN>` header, Health Metrics data type, batch large imports by date range); InfluxDB mapping (measurement/tags/fields/timestamp, dedup by overwrite); link to `testdata/sample-data.json`; endpoints and status codes; how to run unit and integration tests.
- [ ] No statement in the README contradicts the spec.

**Verify:** `grep -cE 'INGEST_TOKEN|INFLUX_URL|INFLUX_TOKEN|INFLUX_ORG|INFLUX_BUCKET|MAX_BODY_BYTES' README.md` → ≥ 6, and a read-through against the spec.

**Steps:**

- [ ] **Step 1: Write `README.md`**

````markdown
# apple-health-ingestor

Receives health metrics from the iOS app [Health Auto Export](https://www.healthyapps.dev/)
(REST API automation) and writes them to InfluxDB 2.x.

## Endpoints

| Method | Path | Auth | Description |
|---|---|---|---|
| `POST` | `/ingest` | `Authorization: Bearer <INGEST_TOKEN>` | Ingest a Health Auto Export JSON payload |
| `GET` | `/healthz` | none | Liveness (does not check InfluxDB) |

`/ingest` responds `200 {"written":N,"skipped":M}`, `401` for a bad token, `400` for
malformed JSON, `413` when the body exceeds `MAX_BODY_BYTES`, and `503` when InfluxDB
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

### Docker Compose (ingestor + InfluxDB)

```bash
cp .env.example .env   # edit the values
docker compose up -d
```

If you already run InfluxDB, remove the `influxdb` service from `docker-compose.yml`
and set `INFLUX_URL` to your instance.

### TLS

The service does not terminate TLS. Put a reverse proxy in front of it, e.g. Caddy:

```
health.example.com {
	reverse_proxy apple-health-ingestor:8080
}
```

## Health Auto Export setup

Create a **REST API** automation in Health Auto Export:

- **URL:** `https://<your-host>/ingest`
- **Headers:** `Authorization` = `Bearer <INGEST_TOKEN>`
- **Data type:** Health Metrics (other data types are ignored)
- **Export format:** JSON

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
````

- [ ] **Step 2: Verify** — run the grep from **Verify** and re-read against `docs/specs/2026-10-04-apple-health-ingestor-design.md`.

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "Document usage, configuration and data model"
```
