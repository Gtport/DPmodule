package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/Gtport/DPmodule/internal/config"
	"github.com/Gtport/DPmodule/pkg/middleware"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func newTestRouter(kc *middleware.KeycloakJWT) *gin.Engine {
	r := gin.New()
	r.Use(kc.Middleware())
	r.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	return r
}

func TestKeycloakJWT_MissingHeader(t *testing.T) {
	kc := middleware.NewKeycloakJWT(config.Keycloak{
		JWKSURL: "http://localhost/jwks",
		Issuer:  "http://localhost/realms/test",
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	newTestRouter(kc).ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestKeycloakJWT_MalformedHeader(t *testing.T) {
	kc := middleware.NewKeycloakJWT(config.Keycloak{
		JWKSURL: "http://localhost/jwks",
		Issuer:  "http://localhost/realms/test",
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "NotBearer something")
	rr := httptest.NewRecorder()

	newTestRouter(kc).ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}
