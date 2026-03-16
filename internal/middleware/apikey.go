// Package middleware provides Gin middleware for the health ingestion service.
package middleware

import (
	"crypto/subtle"
	"log/slog"

	"github.com/gin-gonic/gin"
)

// APIKeyAuth returns a Gin middleware that enforces X-API-Key header authentication.
// It uses constant-time comparison to prevent timing-based attacks.
// Missing key responds 401; incorrect key responds 403.
func APIKeyAuth(key string) gin.HandlerFunc {
	return func(c *gin.Context) {
		submitted := c.GetHeader("X-API-Key")
		if submitted == "" {
			c.AbortWithStatusJSON(401, gin.H{"error": "missing API key"})
			return
		}
		if subtle.ConstantTimeCompare([]byte(submitted), []byte(key)) != 1 {
			slog.Warn("invalid API key attempt", "remote_ip", c.ClientIP())
			c.AbortWithStatusJSON(403, gin.H{"error": "invalid API key"})
			return
		}
		c.Next()
	}
}
