package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	"health-ingestion/internal/config"
	"health-ingestion/internal/handler"
	"health-ingestion/internal/influx"
	"health-ingestion/internal/middleware"
)

// version is set at build time via ldflags: -X main.version=<git-sha>
var version = "dev"

func main() {
	// Load .env if present — harmless in production where env vars are injected directly.
	_ = godotenv.Load()

	// Structured JSON logging for production; slog defaults to text in dev.
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config error", "error", err)
		os.Exit(1)
	}

	writer, err := influx.NewWriter(cfg)
	if err != nil {
		slog.Error("influxdb connection failed", "error", err)
		os.Exit(1)
	}

	handler.Version = version

	r := gin.New()
	r.Use(gin.Recovery())

	r.GET("/health", handler.Health)

	ingestHandler := &handler.IngestHandler{Writer: writer}
	r.POST("/ingest",
		middleware.APIKeyAuth(cfg.IngestionAPIKey),
		ingestHandler.Ingest,
	)

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: r,
	}

	go func() {
		slog.Info("server starting", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutdown signal received")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("shutdown error", "error", err)
	}
	writer.Close()
	slog.Info("server stopped")
}
