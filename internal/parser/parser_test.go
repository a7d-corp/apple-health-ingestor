package parser

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"health-ingestion/internal/model"
)

// helpers

func floatPtr(f float64) *float64 { return &f }

func quantityUnit(qty float64, units string) *model.QuantityUnit {
	return &model.QuantityUnit{Qty: qty, Units: units}
}

// ParseHAEDate tests

func TestParseHAEDate_Valid(t *testing.T) {
	ts, err := ParseHAEDate("2026-03-15 08:32:00 -0500")
	require.NoError(t, err)
	// -0500 is UTC-5, so 08:32 local == 13:32 UTC
	assert.Equal(t, 2026, ts.UTC().Year())
	assert.Equal(t, time.March, ts.UTC().Month())
	assert.Equal(t, 15, ts.UTC().Day())
	assert.Equal(t, 13, ts.UTC().Hour())
	assert.Equal(t, 32, ts.UTC().Minute())
}

func TestParseHAEDate_Invalid(t *testing.T) {
	_, err := ParseHAEDate("not-a-date")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not-a-date")
}

// BuildHealthPoints tests

func TestBuildHealthPoints_Scalar(t *testing.T) {
	metrics := []model.Metric{
		{
			Name:  "step_count",
			Units: "count",
			Data: []model.MetricEntry{
				{Date: "2026-03-14 09:00:00 +0000", Qty: floatPtr(1234)},
			},
		},
	}
	points, errs := BuildHealthPoints(metrics)
	require.Empty(t, errs)
	require.Len(t, points, 1)

	p := points[0]
	assert.Equal(t, "health", p.Name())

	// Check tags
	tags := make(map[string]string)
	for _, tag := range p.TagList() {
		tags[tag.Key] = tag.Value
	}
	assert.Equal(t, "step_count", tags["metric"])
	assert.Equal(t, "count", tags["unit"])
	assert.Equal(t, "apple_health", tags["source"])

	// Check fields
	fields := make(map[string]interface{})
	for _, f := range p.FieldList() {
		fields[f.Key] = f.Value
	}
	assert.InDelta(t, 1234.0, fields["value"], 0.001)
	_, hasAvg := fields["avg"]
	assert.False(t, hasAvg)
}

func TestBuildHealthPoints_MultiStat(t *testing.T) {
	avg := 72.0
	min := 55.0
	max := 95.0
	metrics := []model.Metric{
		{
			Name:  "heart_rate",
			Units: "count/min",
			Data: []model.MetricEntry{
				{Date: "2026-03-14 09:00:00 +0000", Avg: &avg, Min: &min, Max: &max},
			},
		},
	}
	points, errs := BuildHealthPoints(metrics)
	require.Empty(t, errs)
	require.Len(t, points, 1)

	fields := make(map[string]interface{})
	for _, f := range points[0].FieldList() {
		fields[f.Key] = f.Value
	}
	assert.InDelta(t, 72.0, fields["avg"], 0.001)
	assert.InDelta(t, 55.0, fields["min"], 0.001)
	assert.InDelta(t, 95.0, fields["max"], 0.001)
	_, hasValue := fields["value"]
	assert.False(t, hasValue)
}

func TestBuildHealthPoints_MissingQtyAndStats(t *testing.T) {
	metrics := []model.Metric{
		{
			Name:  "blood_oxygen",
			Units: "%",
			Data: []model.MetricEntry{
				{Date: "2026-03-14 09:00:00 +0000"},
			},
		},
	}
	points, errs := BuildHealthPoints(metrics)
	assert.Empty(t, points)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), "no numeric fields present")
}

func TestBuildHealthPoints_BadDate(t *testing.T) {
	metrics := []model.Metric{
		{
			Name:  "heart_rate",
			Units: "count/min",
			Data: []model.MetricEntry{
				{Date: "2026/03/15", Qty: floatPtr(60)},
			},
		},
	}
	points, errs := BuildHealthPoints(metrics)
	assert.Empty(t, points)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), "2026/03/15")
}

func TestBuildHealthPoints_MultipleMetrics(t *testing.T) {
	avg1 := 70.0
	min1 := 50.0
	max1 := 90.0
	metrics := []model.Metric{
		{
			Name:  "heart_rate",
			Units: "count/min",
			Data: []model.MetricEntry{
				{Date: "2026-03-14 09:00:00 +0000", Avg: &avg1, Min: &min1, Max: &max1},
				{Date: "2026-03-14 10:00:00 +0000", Avg: &avg1, Min: &min1, Max: &max1},
				{Date: "2026-03-14 11:00:00 +0000", Avg: &avg1, Min: &min1, Max: &max1},
			},
		},
		{
			Name:  "step_count",
			Units: "count",
			Data: []model.MetricEntry{
				{Date: "2026-03-14 09:00:00 +0000", Qty: floatPtr(500)},
				{Date: "2026-03-14 10:00:00 +0000", Qty: floatPtr(600)},
				{Date: "2026-03-14 11:00:00 +0000", Qty: floatPtr(700)},
			},
		},
	}
	points, errs := BuildHealthPoints(metrics)
	assert.Empty(t, errs)
	assert.Len(t, points, 6)
}

// BuildWorkoutPoints tests

func TestBuildWorkoutPoints_AllFields(t *testing.T) {
	dur := 2735.0
	workouts := []model.Workout{
		{
			Name:               "Outdoor Run",
			Start:              "2026-03-10 19:27:48 +0000",
			End:                "2026-03-10 20:13:23 +0000",
			Duration:           &dur,
			Distance:           quantityUnit(3.1537, "km"),
			ActiveEnergyBurned: quantityUnit(694.66, "kJ"),
			AvgHeartRate:       quantityUnit(170.27, "count/min"),
			MaxHeartRate:       quantityUnit(184.0, "count/min"),
		},
	}
	points, errs := BuildWorkoutPoints(workouts)
	require.Empty(t, errs)
	require.Len(t, points, 1)

	p := points[0]
	assert.Equal(t, "workout", p.Name())

	tags := make(map[string]string)
	for _, tag := range p.TagList() {
		tags[tag.Key] = tag.Value
	}
	assert.Equal(t, "Outdoor Run", tags["type"])
	assert.Equal(t, "apple_health", tags["source"])

	fields := make(map[string]interface{})
	for _, f := range p.FieldList() {
		fields[f.Key] = f.Value
	}
	assert.InDelta(t, 2735.0, fields["duration_sec"], 0.001)
	assert.InDelta(t, 3.1537, fields["distance_km"], 0.0001)
	assert.InDelta(t, 694.66, fields["energy_kj"], 0.001)
	assert.InDelta(t, 170.27, fields["avg_hr"], 0.001)
	assert.InDelta(t, 184.0, fields["max_hr"], 0.001)
}

func TestBuildWorkoutPoints_PartialFields(t *testing.T) {
	dur := 3600.0
	workouts := []model.Workout{
		{
			Name:     "Indoor Cycling",
			Start:    "2026-03-11 07:00:00 +0000",
			Duration: &dur,
		},
	}
	points, errs := BuildWorkoutPoints(workouts)
	require.Empty(t, errs)
	require.Len(t, points, 1)

	fields := make(map[string]interface{})
	for _, f := range points[0].FieldList() {
		fields[f.Key] = f.Value
	}
	assert.InDelta(t, 3600.0, fields["duration_sec"], 0.001)
	_, hasDistance := fields["distance_km"]
	assert.False(t, hasDistance)
	_, hasHR := fields["avg_hr"]
	assert.False(t, hasHR)
}

func TestBuildWorkoutPoints_NoFields(t *testing.T) {
	workouts := []model.Workout{
		{
			Name:  "Unknown Activity",
			Start: "2026-03-11 07:00:00 +0000",
		},
	}
	points, errs := BuildWorkoutPoints(workouts)
	assert.Empty(t, points)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), "no numeric fields present")
}
