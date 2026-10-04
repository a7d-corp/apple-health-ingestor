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
