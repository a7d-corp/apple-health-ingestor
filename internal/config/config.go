// Package config handles loading and validating configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds all runtime configuration for the service.
type Config struct {
	InfluxURL             string
	InfluxToken           string
	InfluxOrg             string
	InfluxBucket          string
	IngestionAPIKey       string
	Port                  string
	InfluxBatchSize       int
	InfluxFlushIntervalMS int
	InfluxMaxRetries      int
	InfluxRetryIntervalMS int
}

// Load reads configuration from environment variables.
// It returns an error listing all missing required variables so the caller can
// fail fast with a single descriptive message.
func Load() (Config, error) {
	cfg := Config{}
	var missing []string

	required := func(key string) string {
		v := os.Getenv(key)
		if v == "" {
			missing = append(missing, key)
		}
		return v
	}

	cfg.InfluxURL = required("INFLUX_URL")
	cfg.InfluxToken = required("INFLUX_TOKEN")
	cfg.InfluxOrg = required("INFLUX_ORG")
	cfg.InfluxBucket = required("INFLUX_BUCKET")
	cfg.IngestionAPIKey = required("INGESTION_API_KEY")

	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing required environment variables: %v", missing)
	}

	cfg.Port = os.Getenv("PORT")
	if cfg.Port == "" {
		cfg.Port = "8080"
	}

	cfg.InfluxBatchSize = envInt("INFLUX_BATCH_SIZE", 500)
	cfg.InfluxFlushIntervalMS = envInt("INFLUX_FLUSH_INTERVAL_MS", 1000)
	cfg.InfluxMaxRetries = envInt("INFLUX_MAX_RETRIES", 3)
	cfg.InfluxRetryIntervalMS = envInt("INFLUX_RETRY_INTERVAL_MS", 500)

	return cfg, nil
}

// envInt reads an integer environment variable, returning def if absent or unparseable.
func envInt(key string, def int) int {
	s := os.Getenv(key)
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return v
}
