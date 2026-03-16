// Package handler contains the HTTP handler functions for the health ingestion API.
package handler

import (
	"github.com/gin-gonic/gin"
)

// Version is injected at build time via ldflags: -X main.version=<sha>.
// The main package sets handler.Version = version after loading it from the linker.
var Version = "dev"

// Health handles GET /health and returns a simple liveness response.
// It is unauthenticated and is suitable for use as a load-balancer health check.
func Health(c *gin.Context) {
	c.JSON(200, gin.H{"status": "ok", "version": Version})
}
