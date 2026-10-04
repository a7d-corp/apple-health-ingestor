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
