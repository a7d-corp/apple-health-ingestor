package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func newTestRouter(key string) *gin.Engine {
	r := gin.New()
	r.GET("/protected", APIKeyAuth(key), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	return r
}

func TestAPIKeyAuth_Missing(t *testing.T) {
	r := newTestRouter("secret-key")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAPIKeyAuth_Wrong(t *testing.T) {
	r := newTestRouter("secret-key")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("X-API-Key", "wrong-key")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestAPIKeyAuth_Correct(t *testing.T) {
	r := newTestRouter("secret-key")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("X-API-Key", "secret-key")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}
