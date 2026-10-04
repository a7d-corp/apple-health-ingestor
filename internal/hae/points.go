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
