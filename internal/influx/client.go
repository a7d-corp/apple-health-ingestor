// Package influx wraps the InfluxDB v2 client and exposes a Writer interface
// so handlers can be tested without a live InfluxDB instance.
package influx

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api/write"

	"health-ingestion/internal/config"
)

// Writer is the interface that the ingest handler depends on.
// Using an interface allows the handler to be tested with a mock.
type Writer interface {
	WritePoints(ctx context.Context, points []*write.Point) error
	Close()
}

// InfluxWriter is the production implementation of Writer backed by InfluxDB v2.
type InfluxWriter struct {
	client influxdb2.Client
	cfg    config.Config
}

// NewWriter creates and validates a new InfluxWriter.
// It pings InfluxDB with a 5-second timeout and returns an error if unreachable.
func NewWriter(cfg config.Config) (*InfluxWriter, error) {
	client := influxdb2.NewClientWithOptions(cfg.InfluxURL, cfg.InfluxToken,
		influxdb2.DefaultOptions().
			SetBatchSize(uint(cfg.InfluxBatchSize)).
			SetFlushInterval(uint(cfg.InfluxFlushIntervalMS)).
			SetMaxRetries(uint(cfg.InfluxMaxRetries)).
			SetRetryInterval(uint(cfg.InfluxRetryIntervalMS)))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ok, err := client.Ping(ctx)
	if err != nil || !ok {
		client.Close()
		if err == nil {
			err = fmt.Errorf("ping returned false")
		}
		return nil, fmt.Errorf("influxdb unreachable: %w", err)
	}

	return &InfluxWriter{client: client, cfg: cfg}, nil
}

// WritePoints writes points to InfluxDB in batches, retrying on transient errors.
// It splits the points slice into chunks of at most cfg.InfluxBatchSize and writes
// each chunk sequentially. On failure, it retries up to cfg.InfluxMaxRetries times
// with cfg.InfluxRetryIntervalMS delay between attempts.
func (w *InfluxWriter) WritePoints(ctx context.Context, points []*write.Point) error {
	writeAPI := w.client.WriteAPIBlocking(w.cfg.InfluxOrg, w.cfg.InfluxBucket)

	batchSize := w.cfg.InfluxBatchSize
	if batchSize <= 0 {
		batchSize = 500
	}

	for start := 0; start < len(points); start += batchSize {
		end := start + batchSize
		if end > len(points) {
			end = len(points)
		}
		chunk := points[start:end]

		var lastErr error
		for attempt := 0; attempt <= w.cfg.InfluxMaxRetries; attempt++ {
			if attempt > 0 {
				slog.Warn("retrying influx write", "attempt", attempt, "error", lastErr)
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(time.Duration(w.cfg.InfluxRetryIntervalMS) * time.Millisecond):
				}
			}
			if err := writeAPI.WritePoint(ctx, chunk...); err != nil {
				lastErr = err
				continue
			}
			lastErr = nil
			break
		}
		if lastErr != nil {
			return fmt.Errorf("influx write failed after %d retries: %w", w.cfg.InfluxMaxRetries, lastErr)
		}
	}
	return nil
}

// Close flushes pending writes and closes the underlying InfluxDB client.
func (w *InfluxWriter) Close() {
	w.client.Close()
}
