package handler

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"

	"health-ingestion/internal/influx"
	"health-ingestion/internal/model"
	"health-ingestion/internal/parser"
)

var validate = validator.New()

// IngestHandler handles POST /ingest requests.
type IngestHandler struct {
	Writer influx.Writer
}

// Ingest decodes a Health Auto Export JSON payload, converts it to InfluxDB write points,
// and writes them in a single batched call. Parser errors are non-fatal and are returned
// in the response body so the iOS client can log them.
func (h *IngestHandler) Ingest(c *gin.Context) {
	// Limit request body to 10 MB.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 10<<20)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	start := time.Now()

	var payload model.HAEPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := validate.Struct(payload); err != nil {
		var details []string
		for _, e := range err.(validator.ValidationErrors) {
			details = append(details, e.Namespace()+" "+e.Tag())
		}
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error":   "validation failed",
			"details": details,
		})
		return
	}

	healthPoints, healthErrs := parser.BuildHealthPoints(payload.Data.Metrics)
	workoutPoints, workoutErrs := parser.BuildWorkoutPoints(payload.Data.Workouts)

	allPoints := append(healthPoints, workoutPoints...)
	allErrs := append(healthErrs, workoutErrs...)

	parserErrResponses := make([]gin.H, 0, len(allErrs))
	for _, e := range allErrs {
		parserErrResponses = append(parserErrResponses, gin.H{
			"context": e.Context,
			"error":   e.Err.Error(),
		})
	}

	if len(allPoints) == 0 {
		c.JSON(http.StatusOK, gin.H{"inserted": 0, "warnings": parserErrResponses})
		return
	}

	if err := h.Writer.WritePoints(ctx, allPoints); err != nil {
		slog.Error("influx write error", "error", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "upstream write failed"})
		return
	}

	duration := time.Since(start)
	slog.Info("ingest complete",
		"remote_ip", c.ClientIP(),
		"metrics", len(payload.Data.Metrics),
		"workouts", len(payload.Data.Workouts),
		"points", len(allPoints),
		"duration_ms", duration.Milliseconds(),
	)

	c.JSON(http.StatusOK, gin.H{
		"inserted":      len(allPoints),
		"parser_errors": parserErrResponses,
	})
}
