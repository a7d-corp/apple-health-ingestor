// Package parser converts validated HAE model structs into InfluxDB write points.
// It is pure — no I/O, no global state — and is fully unit-testable.
package parser

import (
	"fmt"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api/write"

	"health-ingestion/internal/model"
)

// haeLayout is the date format used by Health Auto Export timestamps.
const haeLayout = "2006-01-02 15:04:05 -0700"

// ParserError records a non-fatal error encountered while processing a single entry.
type ParserError struct {
	Context string // e.g. "metric heart_rate entry 3"
	Err     error
}

// Error implements the error interface.
func (e ParserError) Error() string {
	return fmt.Sprintf("%s: %v", e.Context, e.Err)
}

// ParseHAEDate parses a Health Auto Export timestamp string into a time.Time.
// It returns a descriptive error containing the raw input string on failure.
func ParseHAEDate(s string) (time.Time, error) {
	t, err := time.Parse(haeLayout, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("cannot parse date: %q", s)
	}
	return t, nil
}

// BuildHealthPoints converts a slice of Metric values into InfluxDB write points.
// Non-fatal errors (bad date, no numeric fields) are collected and returned alongside
// any successfully built points so callers can process partial batches.
func BuildHealthPoints(metrics []model.Metric) ([]*write.Point, []ParserError) {
	var points []*write.Point
	var errs []ParserError

	for _, m := range metrics {
		for i, e := range m.Data {
			ctx := fmt.Sprintf("metric %s entry %d", m.Name, i)
			t, err := ParseHAEDate(e.Date)
			if err != nil {
				errs = append(errs, ParserError{Context: ctx, Err: err})
				continue
			}

			p := influxdb2.NewPointWithMeasurement("health").
				AddTag("metric", m.Name).
				AddTag("unit", m.Units).
				AddTag("source", "apple_health").
				SetTime(t)

			if e.Qty != nil {
				p.AddField("value", *e.Qty)
				points = append(points, p)
			} else if e.Avg != nil || e.Min != nil || e.Max != nil {
				if e.Avg != nil {
					p.AddField("avg", *e.Avg)
				}
				if e.Min != nil {
					p.AddField("min", *e.Min)
				}
				if e.Max != nil {
					p.AddField("max", *e.Max)
				}
				points = append(points, p)
			} else {
				errs = append(errs, ParserError{
					Context: ctx,
					Err:     fmt.Errorf("no numeric fields present"),
				})
			}
		}
	}
	return points, errs
}

// BuildWorkoutPoints converts a slice of Workout values into InfluxDB write points.
// Workouts with no numeric fields are skipped and a ParserError is recorded.
func BuildWorkoutPoints(workouts []model.Workout) ([]*write.Point, []ParserError) {
	var points []*write.Point
	var errs []ParserError

	for i, w := range workouts {
		ctx := fmt.Sprintf("workout %s index %d", w.Name, i)
		t, err := ParseHAEDate(w.Start)
		if err != nil {
			errs = append(errs, ParserError{Context: ctx, Err: err})
			continue
		}

		p := influxdb2.NewPointWithMeasurement("workout").
			AddTag("type", w.Name).
			AddTag("source", "apple_health").
			SetTime(t)

		hasField := false
		if w.Duration != nil {
			p.AddField("duration_sec", *w.Duration)
			hasField = true
		}
		if w.Distance != nil {
			p.AddField("distance_km", w.Distance.Qty)
			hasField = true
		}
		if w.ActiveEnergyBurned != nil {
			p.AddField("energy_kj", w.ActiveEnergyBurned.Qty)
			hasField = true
		}
		if w.AvgHeartRate != nil {
			p.AddField("avg_hr", w.AvgHeartRate.Qty)
			hasField = true
		}
		if w.MaxHeartRate != nil {
			p.AddField("max_hr", w.MaxHeartRate.Qty)
			hasField = true
		}

		if !hasField {
			errs = append(errs, ParserError{
				Context: ctx,
				Err:     fmt.Errorf("no numeric fields present"),
			})
			continue
		}
		points = append(points, p)
	}
	return points, errs
}
