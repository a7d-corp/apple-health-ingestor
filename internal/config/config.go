// Package config loads the service configuration from environment variables.
package config

import (
	"fmt"
	"strconv"
	"strings"
)

// DefaultMaxBodyBytes is the default request body limit (100 MiB).
const DefaultMaxBodyBytes = 100 << 20

// Config is the service configuration.
type Config struct {
	IngestToken  string
	InfluxURL    string
	InfluxToken  string
	InfluxOrg    string
	InfluxBucket string
	MaxBodyBytes int64
}

// Load reads the configuration using getenv (normally os.Getenv).
func Load(getenv func(string) string) (Config, error) {
	c := Config{
		IngestToken:  getenv("INGEST_TOKEN"),
		InfluxURL:    getenv("INFLUX_URL"),
		InfluxToken:  getenv("INFLUX_TOKEN"),
		InfluxOrg:    getenv("INFLUX_ORG"),
		InfluxBucket: getenv("INFLUX_BUCKET"),
		MaxBodyBytes: DefaultMaxBodyBytes,
	}

	var missing []string
	for _, v := range []struct{ name, value string }{
		{"INGEST_TOKEN", c.IngestToken},
		{"INFLUX_URL", c.InfluxURL},
		{"INFLUX_TOKEN", c.InfluxToken},
		{"INFLUX_ORG", c.InfluxOrg},
		{"INFLUX_BUCKET", c.InfluxBucket},
	} {
		if v.value == "" {
			missing = append(missing, v.name)
		}
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}

	if s := getenv("MAX_BODY_BYTES"); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n <= 0 {
			return Config{}, fmt.Errorf("MAX_BODY_BYTES must be a positive integer, got %q", s)
		}
		c.MaxBodyBytes = n
	}
	return c, nil
}
