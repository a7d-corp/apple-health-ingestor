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
