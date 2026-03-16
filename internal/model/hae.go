// Package model defines the data structures for Health Auto Export (HAE) payloads.
package model

// QuantityUnit represents HAE's {qty, units} object fields used in workout metrics.
type QuantityUnit struct {
	Qty   float64 `json:"qty"`
	Units string  `json:"units"`
}

// HAEPayload is the top-level envelope of a Health Auto Export JSON payload.
type HAEPayload struct {
	Data HAEData `json:"data" validate:"required"`
}

// HAEData contains the metrics and workouts arrays from the HAE payload.
type HAEData struct {
	Metrics  []Metric  `json:"metrics"  validate:"dive"`
	Workouts []Workout `json:"workouts" validate:"dive"`
}

// Metric represents a single metric category (e.g. heart_rate, step_count).
type Metric struct {
	Name  string        `json:"name"  validate:"required,min=1"`
	Units string        `json:"units"`
	Data  []MetricEntry `json:"data"  validate:"required,dive"`
}

// MetricEntry is one time-stamped data point within a metric.
// It is valid if Qty != nil OR at least one of Avg/Min/Max != nil.
type MetricEntry struct {
	Date string   `json:"date" validate:"required"`
	Qty  *float64 `json:"qty"`
	Avg  *float64 `json:"Avg"`
	Min  *float64 `json:"Min"`
	Max  *float64 `json:"Max"`
	// source, start, end are present in the payload but not used for ingestion
}

// Workout represents a single workout session.
// AvgHeartRate and MaxHeartRate can be absent (null) in the JSON.
type Workout struct {
	Name               string        `json:"name"               validate:"required"`
	Start              string        `json:"start"              validate:"required"`
	End                string        `json:"end"`
	Duration           *float64      `json:"duration"`
	Distance           *QuantityUnit `json:"distance"`
	ActiveEnergyBurned *QuantityUnit `json:"activeEnergyBurned"`
	AvgHeartRate       *QuantityUnit `json:"avgHeartRate"`
	MaxHeartRate       *QuantityUnit `json:"maxHeartRate"`
}
