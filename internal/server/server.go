// Package server implements the HTTP API of the ingestor.
package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/a7d-corp/apple-health-ingestor/internal/hae"
)

// PointWriter persists points.
type PointWriter interface {
	Write(ctx context.Context, points []hae.Point) error
}

// New returns the handler serving POST /ingest and GET /healthz.
func New(token string, maxBodyBytes int64, writer PointWriter, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("POST /ingest", &ingestHandler{
		want:         []byte("Bearer " + token),
		maxBodyBytes: maxBodyBytes,
		writer:       writer,
		log:          log,
	})
	return mux
}

type ingestHandler struct {
	want         []byte
	maxBodyBytes int64
	writer       PointWriter
	log          *slog.Logger
}

func (h *ingestHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), h.want) != 1 {
		h.log.Warn("unauthorized ingest request", "remote", r.RemoteAddr)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	payload, err := hae.Decode(http.MaxBytesReader(w, r.Body, h.maxBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			h.log.Warn("request body too large", "limit", h.maxBodyBytes)
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			h.log.Warn("timed out reading request body", "error", err)
			http.Error(w, "request timeout", http.StatusRequestTimeout)
			return
		}
		h.log.Warn("malformed payload", "error", err)
		http.Error(w, "malformed payload", http.StatusBadRequest)
		return
	}

	for _, key := range payload.Ignored {
		h.log.Info("ignoring unsupported data type", "key", key)
	}
	points, skips := hae.ToPoints(payload.Metrics)
	for _, s := range skips {
		h.log.Warn("skipping entry", "metric", s.Metric, "index", s.Index, "reason", s.Reason)
	}

	if err := h.writer.Write(r.Context(), points); err != nil {
		h.log.Error("writing points", "error", err, "points", len(points))
		http.Error(w, "failed to write points", http.StatusServiceUnavailable)
		return
	}

	h.log.Info("ingested payload", "written", len(points), "skipped", len(skips), "duration", time.Since(start))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int{"written": len(points), "skipped": len(skips)})
}
