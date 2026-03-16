package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/influxdata/influxdb-client-go/v2/api/write"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"health-ingestion/internal/middleware"
)

const testAPIKey = "test-api-key"

// mockWriter is a test double that records calls to WritePoints.
type mockWriter struct {
	returnErr  error
	callCount  int
	pointCount int
}

func (m *mockWriter) WritePoints(_ context.Context, points []*write.Point) error {
	m.callCount++
	m.pointCount += len(points)
	return m.returnErr
}

func (m *mockWriter) Close() {}

func newTestIngestRouter(w *mockWriter) *gin.Engine {
	r := gin.New()
	h := &IngestHandler{Writer: w}
	r.POST("/ingest",
		middleware.APIKeyAuth(testAPIKey),
		h.Ingest,
	)
	return r
}

func loadTestdata(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("../../testdata/" + name)
	require.NoError(t, err, "failed to read testdata/%s", name)
	return data
}

func postJSON(r *gin.Engine, body []byte, apiKey string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/ingest", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	r.ServeHTTP(w, req)
	return w
}

func TestIngest_ValidFullPayload(t *testing.T) {
	mock := &mockWriter{}
	r := newTestIngestRouter(mock)

	body := loadTestdata(t, "payload_full.json")
	w := postJSON(r, body, testAPIKey)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	inserted, ok := resp["inserted"].(float64)
	require.True(t, ok, "inserted field missing or wrong type")
	assert.Greater(t, inserted, float64(0))
	assert.Equal(t, 1, mock.callCount)
}

func TestIngest_MetricsOnly(t *testing.T) {
	mock := &mockWriter{}
	r := newTestIngestRouter(mock)

	body := loadTestdata(t, "payload_metrics_only.json")
	w := postJSON(r, body, testAPIKey)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	inserted := resp["inserted"].(float64)
	assert.Greater(t, inserted, float64(0))
}

func TestIngest_WorkoutsOnly(t *testing.T) {
	mock := &mockWriter{}
	r := newTestIngestRouter(mock)

	body := loadTestdata(t, "payload_workouts_only.json")
	w := postJSON(r, body, testAPIKey)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	inserted := resp["inserted"].(float64)
	assert.Greater(t, inserted, float64(0))
}

func TestIngest_EmptyData(t *testing.T) {
	mock := &mockWriter{}
	r := newTestIngestRouter(mock)

	body := []byte(`{"data": {}}`)
	w := postJSON(r, body, testAPIKey)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(0), resp["inserted"])
	assert.Equal(t, 0, mock.callCount)
}

func TestIngest_MalformedJSON(t *testing.T) {
	mock := &mockWriter{}
	r := newTestIngestRouter(mock)

	body := loadTestdata(t, "payload_malformed.json")
	w := postJSON(r, body, testAPIKey)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, 0, mock.callCount)
}

func TestIngest_ValidationFailure(t *testing.T) {
	mock := &mockWriter{}
	r := newTestIngestRouter(mock)

	// Missing "data" key entirely — the struct tag validate:"required" on Data will fire,
	// but since HAEData is a value type the zero-value is valid to the required tag.
	// Instead, post a metric with an empty name to trigger min=1.
	body := []byte(`{"data": {"metrics": [{"name": "", "units": "count", "data": [{"date": "2026-03-14 09:00:00 +0000", "qty": 1}]}]}}`)
	w := postJSON(r, body, testAPIKey)

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "validation failed", resp["error"])
	_, hasDetails := resp["details"]
	assert.True(t, hasDetails)
}

func TestIngest_WriterError(t *testing.T) {
	mock := &mockWriter{returnErr: errors.New("connection refused")}
	r := newTestIngestRouter(mock)

	body := loadTestdata(t, "payload_full.json")
	w := postJSON(r, body, testAPIKey)

	assert.Equal(t, http.StatusBadGateway, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "upstream write failed", resp["error"])
}

func TestIngest_NoAPIKey(t *testing.T) {
	mock := &mockWriter{}
	r := newTestIngestRouter(mock)

	body := loadTestdata(t, "payload_full.json")
	w := postJSON(r, body, "") // no key

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, 0, mock.callCount)
}
