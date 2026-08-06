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
	// Как в server.go: аутентификация + гейт «мутации — набору правок» на всю группу.
	api := r.Group("/api", kc.Middleware(), kc.RequireForWrites(auth.AccessWrite))
	api.GET("/me", func(c *gin.Context) {
		cl := auth.ClaimsFromContext(c.Request.Context())
		c.JSON(http.StatusOK, gin.H{"username": cl.Username, "sub": cl.Subject})
	})
	api.POST("/edit", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	api.GET("/admin", kc.Require(auth.AccessAdmin), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	return r
}

func doReq(kc *middleware.KeycloakJWT, method, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	newAuthRouter(kc).ServeHTTP(rr, req)
	return rr
}

func doGet(kc *middleware.KeycloakJWT, path, token string) *httptest.ResponseRecorder {
	return doReq(kc, http.MethodGet, path, token)
}

func TestKeycloakJWT_RoundTrip(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	srv := jwksServer(t, testKid, &key.PublicKey)
	// ClientID заполняет config.Load (дефолт из audience); здесь конфиг собирается
	// руками, поэтому задаём явно — по нему читаются client-роли из resource_access.
	cfg := config.Keycloak{JWKSURL: srv.URL, Issuer: testIssuer, Audience: testAudience, ClientID: testAudience}

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

	t.Run("RequireAnyRole: operator (legacy dispatcher) hits /admin → 403", func(t *testing.T) {
		rr := doGet(middleware.NewKeycloakJWT(cfg), "/api/admin", mintToken(t, key, testKid, baseClaims()))
		assert.Equal(t, http.StatusForbidden, rr.Code)
	})

	t.Run("RequireAnyRole: admin_dpport hits /admin → 200", func(t *testing.T) {
		c := baseClaims()
		c["realm_access"] = map[string]any{"roles": []any{"operator_dpport", "admin_dpport"}}
		rr := doGet(middleware.NewKeycloakJWT(cfg), "/api/admin", mintToken(t, key, testKid, c))
		assert.Equal(t, http.StatusOK, rr.Code)
	})

	t.Run("RequireAnyRole: admin (legacy administrator) hits /admin → 200", func(t *testing.T) {
		c := baseClaims()
		c["realm_access"] = map[string]any{"roles": []any{"dispatcher", "administrator"}}
		rr := doGet(middleware.NewKeycloakJWT(cfg), "/api/admin", mintToken(t, key, testKid, c))
		assert.Equal(t, http.StatusOK, rr.Code)
	})

	// Гейт «порог правок»: чтение — любому залогиненному, мутации — набору Writers.
	t.Run("write-gate: client GET → 200, POST → 403", func(t *testing.T) {
		c := baseClaims()
		c["realm_access"] = map[string]any{"roles": []any{"client"}}
		kc := middleware.NewKeycloakJWT(cfg)
		assert.Equal(t, http.StatusOK, doGet(kc, "/api/me", mintToken(t, key, testKid, c)).Code)
		assert.Equal(t, http.StatusForbidden, doReq(kc, http.MethodPost, "/api/edit", mintToken(t, key, testKid, c)).Code)
	})

	t.Run("write-gate: client_dispatcher POST → 403 (не входит в Writers)", func(t *testing.T) {
		c := baseClaims()
		c["realm_access"] = map[string]any{"roles": []any{"client_dispatcher_dpport"}}
		rr := doReq(middleware.NewKeycloakJWT(cfg), http.MethodPost, "/api/edit", mintToken(t, key, testKid, c))
		assert.Equal(t, http.StatusForbidden, rr.Code)
	})

	t.Run("write-gate: operator_dpport и admin_dpport POST → 200", func(t *testing.T) {
		kc := middleware.NewKeycloakJWT(cfg)
		for _, role := range []string{"operator_dpport", "admin_dpport"} {
			c := baseClaims()
			c["realm_access"] = map[string]any{"roles": []any{role}}
			assert.Equal(t, http.StatusOK,
				doReq(kc, http.MethodPost, "/api/edit", mintToken(t, key, testKid, c)).Code, role)
		}
		// baseClaims несёт legacy-роль dispatcher → нормализуется в operator_dpport.
		assert.Equal(t, http.StatusOK, doReq(kc, http.MethodPost, "/api/edit", mintToken(t, key, testKid, baseClaims())).Code)
	})

	t.Run("write-gate: неизвестная роль POST → 403", func(t *testing.T) {
		c := baseClaims()
		c["realm_access"] = map[string]any{"roles": []any{"manager"}}
		rr := doReq(middleware.NewKeycloakJWT(cfg), http.MethodPost, "/api/edit", mintToken(t, key, testKid, c))
		assert.Equal(t, http.StatusForbidden, rr.Code)
	})

	// strict_roles (общий realm стенда): легаси-роли контура (dispatcher, admin,
	// operator) могут принадлежать пользователям ЧУЖИХ приложений — токен с ними
	// валиден (аутентификация проходит), но наших прав не даёт. Точные *_dpport
	// работают как обычно.
	t.Run("strict_roles: легаси-токен валиден, но прав не даёт", func(t *testing.T) {
		strictCfg := cfg
		strictCfg.StrictRoles = true
		kc := middleware.NewKeycloakJWT(strictCfg)
		legacy := mintToken(t, key, testKid, baseClaims()) // роль dispatcher
		assert.Equal(t, http.StatusOK, doGet(kc, "/api/me", legacy).Code, "чтение — любому залогиненному")
		assert.Equal(t, http.StatusForbidden, doReq(kc, http.MethodPost, "/api/edit", legacy).Code)

		c := baseClaims()
		c["realm_access"] = map[string]any{"roles": []any{"admin", "operator"}}
		foreign := mintToken(t, key, testKid, c)
		assert.Equal(t, http.StatusForbidden, doReq(kc, http.MethodPost, "/api/edit", foreign).Code)
		assert.Equal(t, http.StatusForbidden, doGet(kc, "/api/admin", foreign).Code)

		c = baseClaims()
		c["realm_access"] = map[string]any{"roles": []any{"operator_dpport"}}
		assert.Equal(t, http.StatusOK, doReq(kc, http.MethodPost, "/api/edit", mintToken(t, key, testKid, c)).Code)
	})

	// Client-роли (переход 08.2026): права модуля читаются из
	// resource_access[<наш clientId>].roles — независимо от strict_roles.
	t.Run("client-роли: operator правит, но админки нет", func(t *testing.T) {
		c := baseClaims()
		c["realm_access"] = map[string]any{"roles": []any{}}
		c["resource_access"] = map[string]any{testAudience: map[string]any{"roles": []any{"operator"}}}
		kc := middleware.NewKeycloakJWT(cfg)
		tok := mintToken(t, key, testKid, c)
		assert.Equal(t, http.StatusOK, doReq(kc, http.MethodPost, "/api/edit", tok).Code)
		assert.Equal(t, http.StatusForbidden, doGet(kc, "/api/admin", tok).Code)
	})

	t.Run("client-роли: admin проходит в админку", func(t *testing.T) {
		c := baseClaims()
		c["realm_access"] = map[string]any{"roles": []any{}}
		c["resource_access"] = map[string]any{testAudience: map[string]any{"roles": []any{"admin"}}}
		rr := doGet(middleware.NewKeycloakJWT(cfg), "/api/admin", mintToken(t, key, testKid, c))
		assert.Equal(t, http.StatusOK, rr.Code)
	})

	// ЛОВУШКА СХЕМ: (1) роль operator у ЧУЖОГО клиента в resource_access — не
	// наша; (2) realm-роль operator (легаси контура, strict) — тоже. Пройдут они
	// только у того, кто сложил списки ролей в одну кучу.
	t.Run("client-роли: чужая полка resource_access и realm-легаси не дают прав", func(t *testing.T) {
		strictCfg := cfg
		strictCfg.StrictRoles = true
		c := baseClaims()
		c["realm_access"] = map[string]any{"roles": []any{"operator"}}
		c["resource_access"] = map[string]any{"iqport-rtport": map[string]any{"roles": []any{"operator", "admin"}}}
		kc := middleware.NewKeycloakJWT(strictCfg)
		tok := mintToken(t, key, testKid, c)
		assert.Equal(t, http.StatusOK, doGet(kc, "/api/me", tok).Code, "токен валиден — чтение есть")
		assert.Equal(t, http.StatusForbidden, doReq(kc, http.MethodPost, "/api/edit", tok).Code)
		assert.Equal(t, http.StatusForbidden, doGet(kc, "/api/admin", tok).Code)
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
