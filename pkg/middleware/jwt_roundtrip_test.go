package middleware_test

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/auth"
	"github.com/Gtport/DPmodule/internal/config"
	"github.com/Gtport/DPmodule/pkg/middleware"
)

const (
	testIssuer   = "https://kc.example/realms/iqport"
	testAudience = "iqport-backend"
	testKid      = "test-kid-1"
)

// jwksServer поднимает httptest-сервер, отдающий JWKS с одним публичным ключом.
func jwksServer(t *testing.T, kid string, pub *rsa.PublicKey) *httptest.Server {
	t.Helper()
	n := base64.RawURLEncoding.EncodeToString(pub.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes())
	body := fmt.Sprintf(`{"keys":[{"kid":%q,"kty":"RSA","alg":"RS256","use":"sig","n":%q,"e":%q}]}`, kid, n, e)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// mintToken подписывает JWT приватным ключом с указанным kid.
func mintToken(t *testing.T, key *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid
	s, err := tok.SignedString(key)
	require.NoError(t, err)
	return s
}

// baseClaims — валидный набор claims (нужные поля перекрываются в тестах).
func baseClaims() jwt.MapClaims {
	now := time.Now()
	return jwt.MapClaims{
		"iss":                testIssuer,
		"aud":                testAudience,
		"exp":                now.Add(time.Hour).Unix(),
		"iat":                now.Unix(),
		"sub":                "user-123",
		"preferred_username": "dispatcher1",
		"email":              "d1@example.com",
		"realm_access":       map[string]any{"roles": []any{"dispatcher"}},
	}
}

func newAuthRouter(kc *middleware.KeycloakJWT) *gin.Engine {
	r := gin.New()
	api := r.Group("/api", kc.Middleware())
	api.GET("/me", func(c *gin.Context) {
		cl := auth.ClaimsFromContext(c.Request.Context())
		c.JSON(http.StatusOK, gin.H{"username": cl.Username, "sub": cl.Subject})
	})
	api.GET("/admin", kc.RequireRole(auth.RoleAdministrator), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	return r
}

func doGet(kc *middleware.KeycloakJWT, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	newAuthRouter(kc).ServeHTTP(rr, req)
	return rr
}

func TestKeycloakJWT_RoundTrip(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	srv := jwksServer(t, testKid, &key.PublicKey)
	cfg := config.Keycloak{JWKSURL: srv.URL, Issuer: testIssuer, Audience: testAudience}

	t.Run("valid token → 200 + claims", func(t *testing.T) {
		kc := middleware.NewKeycloakJWT(cfg)
		rr := doGet(kc, "/api/me", mintToken(t, key, testKid, baseClaims()))
		require.Equal(t, http.StatusOK, rr.Code)
		assert.Contains(t, rr.Body.String(), "dispatcher1")
		assert.Contains(t, rr.Body.String(), "user-123")
	})

	t.Run("wrong issuer → 401", func(t *testing.T) {
		c := baseClaims()
		c["iss"] = "https://evil/realms/iqport"
		rr := doGet(middleware.NewKeycloakJWT(cfg), "/api/me", mintToken(t, key, testKid, c))
		assert.Equal(t, http.StatusUnauthorized, rr.Code)
	})

	t.Run("wrong audience → 401", func(t *testing.T) {
		c := baseClaims()
		c["aud"] = "some-other-client"
		rr := doGet(middleware.NewKeycloakJWT(cfg), "/api/me", mintToken(t, key, testKid, c))
		assert.Equal(t, http.StatusUnauthorized, rr.Code)
	})

	t.Run("expired → 401", func(t *testing.T) {
		c := baseClaims()
		c["exp"] = time.Now().Add(-time.Minute).Unix()
		rr := doGet(middleware.NewKeycloakJWT(cfg), "/api/me", mintToken(t, key, testKid, c))
		assert.Equal(t, http.StatusUnauthorized, rr.Code)
	})

	t.Run("bad signature (kid matches, wrong key) → 401", func(t *testing.T) {
		rr := doGet(middleware.NewKeycloakJWT(cfg), "/api/me", mintToken(t, otherKey, testKid, baseClaims()))
		assert.Equal(t, http.StatusUnauthorized, rr.Code)
	})

	t.Run("unknown kid → 401", func(t *testing.T) {
		rr := doGet(middleware.NewKeycloakJWT(cfg), "/api/me", mintToken(t, key, "no-such-kid", baseClaims()))
		assert.Equal(t, http.StatusUnauthorized, rr.Code)
	})

	t.Run("RequireRole: dispatcher hits /admin → 403", func(t *testing.T) {
		rr := doGet(middleware.NewKeycloakJWT(cfg), "/api/admin", mintToken(t, key, testKid, baseClaims()))
		assert.Equal(t, http.StatusForbidden, rr.Code)
	})

	t.Run("RequireRole: administrator hits /admin → 200", func(t *testing.T) {
		c := baseClaims()
		c["realm_access"] = map[string]any{"roles": []any{"dispatcher", "administrator"}}
		rr := doGet(middleware.NewKeycloakJWT(cfg), "/api/admin", mintToken(t, key, testKid, c))
		assert.Equal(t, http.StatusOK, rr.Code)
	})
}

// TestKeycloakJWT_AudienceOptional: пустой Audience в конфиге → aud не проверяется
// (шаблон/дев без audience-mapper остаётся рабочим).
func TestKeycloakJWT_AudienceOptional(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	srv := jwksServer(t, testKid, &key.PublicKey)
	cfg := config.Keycloak{JWKSURL: srv.URL, Issuer: testIssuer} // Audience пуст

	c := baseClaims()
	delete(c, "aud") // токен без audience
	rr := doGet(middleware.NewKeycloakJWT(cfg), "/api/me", mintToken(t, key, testKid, c))
	assert.Equal(t, http.StatusOK, rr.Code)
}
