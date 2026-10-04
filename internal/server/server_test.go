package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/a7d-corp/apple-health-ingestor/internal/hae"
)

const token = "secret"

type fakeWriter struct {
	calls  int
	points []hae.Point
	err    error
}

func (f *fakeWriter) Write(_ context.Context, points []hae.Point) error {
	f.calls++
	f.points = append(f.points, points...)
	return f.err
}

func do(t *testing.T, h http.Handler, method, path, auth string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func newHandler(w PointWriter, maxBody int64) http.Handler {
	return New(token, maxBody, w, slog.New(slog.DiscardHandler))
}

func TestHealthz(t *testing.T) {
	rec := do(t, newHandler(&fakeWriter{}, 1<<20), http.MethodGet, "/healthz", "", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestIngestRejects(t *testing.T) {
	valid := `{"data":{"metrics":[]}}`
	tests := []struct {
		name    string
		method  string
		auth    string
		body    string
		maxBody int64
		want    int
	}{
		{"no auth", http.MethodPost, "", valid, 1 << 20, http.StatusUnauthorized},
		{"wrong token", http.MethodPost, "Bearer nope", valid, 1 << 20, http.StatusUnauthorized},
		{"no bearer prefix", http.MethodPost, token, valid, 1 << 20, http.StatusUnauthorized},
		{"wrong method", http.MethodGet, "Bearer " + token, "", 1 << 20, http.StatusMethodNotAllowed},
		{"malformed", http.MethodPost, "Bearer " + token, `{"data":`, 1 << 20, http.StatusBadRequest},
		{"too large", http.MethodPost, "Bearer " + token, valid, 10, http.StatusRequestEntityTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &fakeWriter{}
			rec := do(t, newHandler(w, tt.maxBody), tt.method, "/ingest", tt.auth, strings.NewReader(tt.body))
			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d", rec.Code, tt.want)
			}
			if w.calls != 0 {
				t.Errorf("writer called %d times, want 0", w.calls)
			}
		})
	}
}

func TestIngestWriterError(t *testing.T) {
	w := &fakeWriter{err: errors.New("influx down")}
	body := `{"data":{"metrics":[{"name":"m","units":"u","data":[{"date":"2025-12-15 09:00:10 +0000","qty":1}]}]}}`
	rec := do(t, newHandler(w, 1<<20), http.MethodPost, "/ingest", "Bearer "+token, strings.NewReader(body))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestIngestSample(t *testing.T) {
	f, err := os.Open("../../testdata/sample-data.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	w := &fakeWriter{}
	rec := do(t, newHandler(w, 100<<20), http.MethodPost, "/ingest", "Bearer "+token, f)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	var got map[string]int
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got["written"] != 3228 || got["skipped"] != 0 {
		t.Errorf("response = %v, want written=3228 skipped=0", got)
	}
	if len(w.points) != 3228 {
		t.Errorf("writer got %d points, want 3228", len(w.points))
	}
}

func TestIngestCountsSkips(t *testing.T) {
	w := &fakeWriter{}
	body := `{"data":{"metrics":[{"name":"m","units":"u","data":[{"qty":1},{"date":"2025-12-15 09:00:10 +0000","qty":2}]}]}}`
	rec := do(t, newHandler(w, 1<<20), http.MethodPost, "/ingest", "Bearer "+token, strings.NewReader(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if want := `{"skipped":1,"written":1}`; strings.TrimSpace(rec.Body.String()) != want {
		t.Errorf("body = %s, want %s", rec.Body, want)
	}
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

type timeoutReader struct{}

func (timeoutReader) Read([]byte) (int, error) { return 0, timeoutError{} }

func TestIngestBodyReadTimeout(t *testing.T) {
	w := &fakeWriter{}
	rec := do(t, newHandler(w, 1<<20), http.MethodPost, "/ingest", "Bearer "+token, timeoutReader{})
	if rec.Code != http.StatusRequestTimeout {
		t.Errorf("status = %d, want 408", rec.Code)
	}
	if w.calls != 0 {
		t.Errorf("writer called %d times, want 0", w.calls)
	}
}
