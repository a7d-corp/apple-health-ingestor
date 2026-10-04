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
