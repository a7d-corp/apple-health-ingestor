package config

import "testing"

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func valid() map[string]string {
	return map[string]string{
		"INGEST_TOKEN":  "secret",
		"INFLUX_URL":    "http://influxdb:8086",
		"INFLUX_TOKEN":  "influx-token",
		"INFLUX_ORG":    "home",
		"INFLUX_BUCKET": "health",
	}
}

func TestLoadValid(t *testing.T) {
	c, err := Load(env(valid()))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Config{
		IngestToken:  "secret",
		InfluxURL:    "http://influxdb:8086",
		InfluxToken:  "influx-token",
		InfluxOrg:    "home",
		InfluxBucket: "health",
		MaxBodyBytes: 104857600,
	}
	if c != want {
		t.Errorf("got %+v, want %+v", c, want)
	}
}

func TestLoadMissing(t *testing.T) {
	m := valid()
	delete(m, "INGEST_TOKEN")
	delete(m, "INFLUX_BUCKET")
	_, err := Load(env(m))
	want := "missing required environment variables: INGEST_TOKEN, INFLUX_BUCKET"
	if err == nil || err.Error() != want {
		t.Errorf("err = %v, want %q", err, want)
	}
}

func TestLoadMaxBodyBytes(t *testing.T) {
	m := valid()
	m["MAX_BODY_BYTES"] = "1024"
	c, err := Load(env(m))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.MaxBodyBytes != 1024 {
		t.Errorf("MaxBodyBytes = %d, want 1024", c.MaxBodyBytes)
	}

	for _, bad := range []string{"abc", "0", "-5"} {
		m["MAX_BODY_BYTES"] = bad
		if _, err := Load(env(m)); err == nil {
			t.Errorf("MAX_BODY_BYTES=%q: expected error", bad)
		}
	}
}
