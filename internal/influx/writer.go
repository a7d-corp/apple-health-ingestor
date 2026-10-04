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
