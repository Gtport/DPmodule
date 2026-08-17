package middleware

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"

	"github.com/Gtport/DPmodule/internal/auth"
	"github.com/Gtport/DPmodule/internal/config"
	"github.com/Gtport/DPmodule/pkg/logger"
)

// jwksMinRefreshInterval — минимальный интервал между обновлениями JWKS при
// встрече неизвестного kid. Защищает Keycloak от шторма запросов и одновременно
// позволяет подхватить ротацию ключей в пределах минуты.
const jwksMinRefreshInterval = time.Minute

// KeycloakJWT validates Bearer tokens against Keycloak's JWKS endpoint.
// Keys are cached and refreshed lazily when a matching kid is not found.
type KeycloakJWT struct {
	cfg    config.Keycloak
	mu     sync.RWMutex
	keys   map[string]*rsa.PublicKey
	loadAt time.Time
}

func NewKeycloakJWT(cfg config.Keycloak) *KeycloakJWT {
	return &KeycloakJWT{cfg: cfg, keys: map[string]*rsa.PublicKey{}}
}

// Middleware returns a gin.HandlerFunc that requires a valid JWT.
// On success it stores *auth.Claims in the request context.
func (k *KeycloakJWT) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		log := logger.FromContext(c.Request.Context())

		raw, err := extractBearer(c.Request)
		if err != nil {
			log.Debug("токена в запросе нет", logger.Comp(logger.CompAuth), zap.Error(err))
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing or malformed Authorization header"})
			return
		}

		claims, err := k.validate(c.Request.Context(), raw)
		if err != nil {
			log.Warn("токен не принят", logger.Comp(logger.CompAuth), zap.Error(err))
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}

		c.Request = c.Request.WithContext(auth.WithClaims(c.Request.Context(), claims))
		c.Next()
	}
}

// Require — доступ тем, кого пускает набор (client-роль ИЛИ realm-роль, каждая
// схема сверяется со своим списком — auth.Access). Иерархии нет (роли
// независимы), набор перечисляется целиком. 401 без claims, 403 без роли.
func (k *KeycloakJWT) Require(a auth.Access) gin.HandlerFunc {
	return func(c *gin.Context) {
		abortUnlessAllowed(c, a)
	}
}

// RequireForWrites — гейт «порог правок»: чтение (GET/HEAD/OPTIONS)
// пропускается как есть (аутентификацию уже проверил Middleware), любая
// мутация (POST/PUT/PATCH/DELETE) требует доступ из набора. Вешается на всю
// группу /api/v1: новые мутирующие ручки закрыты автоматически.
func (k *KeycloakJWT) RequireForWrites(a auth.Access) gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			c.Next()
			return
		}
		abortUnlessAllowed(c, a)
	}
}

func abortUnlessAllowed(c *gin.Context, a auth.Access) {
	cl := auth.ClaimsFromContext(c.Request.Context())
	if cl == nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}
	if !cl.Allows(a) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		return
	}
	c.Next()
}

// ---- internal ----

func extractBearer(r *http.Request) (string, error) {
	hdr := r.Header.Get("Authorization")
	if hdr == "" {
		return "", errors.New("no Authorization header")
	}
	parts := strings.SplitN(hdr, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return "", errors.New("not a Bearer token")
	}
	return parts[1], nil
}

func (k *KeycloakJWT) validate(ctx context.Context, raw string) (*auth.Claims, error) {
	opts := []jwt.ParserOption{
		jwt.WithValidMethods([]string{"RS256"}), // только RSA/RS256 (Keycloak по умолчанию)
		jwt.WithIssuer(k.cfg.Issuer),
		jwt.WithExpirationRequired(),
	}
	// Audience проверяем ТОЛЬКО если он задан в конфиге: пустой audience →
	// шаблон/дев без aud остаётся рабочим. Когда задан — Keycloak обязан класть
	// это значение в claim `aud` (audience-mapper на клиенте), иначе токен
	// отвергается. Закрывает дыру «токен другого клиента проходит».
	if k.cfg.Audience != "" {
		opts = append(opts, jwt.WithAudience(k.cfg.Audience))
	}

	token, err := jwt.Parse(raw, k.keyFunc(ctx), opts...)
	if err != nil {
		return nil, err
	}

	mapClaims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid claims")
	}

	return extractClaims(mapClaims, k.cfg.ClientID, k.cfg.StrictRoles)
}

func (k *KeycloakJWT) keyFunc(ctx context.Context) jwt.Keyfunc {
	return func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		kid, _ := t.Header["kid"].(string)
		return k.getKey(ctx, kid)
	}
}

func (k *KeycloakJWT) getKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	k.mu.RLock()
	key, ok := k.keys[kid]
	k.mu.RUnlock()
	if ok {
		return key, nil
	}

	k.mu.Lock()
	defer k.mu.Unlock()
	// Повторная проверка под write-lock: другой поток мог уже загрузить ключ.
	if key, ok = k.keys[kid]; ok {
		return key, nil
	}
	// Неизвестный kid → обновляем JWKS, но не чаще jwksMinRefreshInterval
	// (rate-limit против шторма запросов с поддельными kid).
	if time.Since(k.loadAt) > jwksMinRefreshInterval {
		if err := k.fetchJWKS(ctx); err != nil {
			return nil, fmt.Errorf("jwks refresh: %w", err)
		}
	}
	key, ok = k.keys[kid]
	if !ok {
		return nil, fmt.Errorf("jwks: no key with kid=%q", kid)
	}
	return key, nil
}

type jwksResponse struct {
	Keys []jwk `json:"keys"`
}

type jwk struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// jwksHTTPClient — отдельный клиент с таймаутом: не даём загрузке JWKS подвесить
// запрос (http.DefaultClient таймаута не имеет). Контекст запроса тоже уважается.
var jwksHTTPClient = &http.Client{Timeout: 10 * time.Second}

func (k *KeycloakJWT) fetchJWKS(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, k.cfg.JWKSURL, nil)
	if err != nil {
		return err
	}
	resp, err := jwksHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jwks: unexpected status %d from %s", resp.StatusCode, k.cfg.JWKSURL)
	}

	var jwks jwksResponse
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return err
	}

	newKeys := make(map[string]*rsa.PublicKey, len(jwks.Keys))
	for _, j := range jwks.Keys {
		if j.Kty != "RSA" {
			continue
		}
		pub, err := jwkToRSA(j)
		if err != nil {
			return fmt.Errorf("jwk kid=%s: %w", j.Kid, err)
		}
		newKeys[j.Kid] = pub
	}
	k.keys = newKeys
	k.loadAt = time.Now()
	return nil
}

func jwkToRSA(j jwk) (*rsa.PublicKey, error) {
	nb, err := base64.RawURLEncoding.DecodeString(j.N)
	if err != nil {
		return nil, err
	}
	eb, err := base64.RawURLEncoding.DecodeString(j.E)
	if err != nil {
		return nil, err
	}
	e := int(new(big.Int).SetBytes(eb).Int64())
	return &rsa.PublicKey{N: new(big.Int).SetBytes(nb), E: e}, nil
}

func extractClaims(m jwt.MapClaims, clientID string, strict bool) (*auth.Claims, error) {
	sub, _ := m["sub"].(string)
	email, _ := m["email"].(string)
	username, _ := m["preferred_username"].(string)

	// Две схемы ролей кладутся в РАЗНЫЕ поля и не смешиваются (auth.Access):
	// client-роли своего клиента — resource_access[<clientId>].roles, дословно;
	// realm-роли — realm_access.roles, через auth.TokenRoles (в strict-режиме
	// общего realm'а — дословно, иначе легаси-имена нормализуются).
	var clientRoles []auth.Role
	if ra, ok := m["resource_access"].(map[string]any); ok {
		if client, ok := ra[clientID].(map[string]any); ok {
			clientRoles = rolesOf(client["roles"])
		}
	}
	var realmRaw any
	if ra, ok := m["realm_access"].(map[string]any); ok {
		realmRaw = ra["roles"]
	}
	roles := auth.TokenRoles(rolesOf(realmRaw), strict)

	return &auth.Claims{
		Subject:  sub,
		Email:    email,
		Username: username,
		Roles:       roles,
		ClientRoles: clientRoles,
	}, nil
}

// rolesOf — []any из JSON-claim'а в []auth.Role; не-строки и чужие типы молча
// пропускаются (битый claim — не наш токен, упадёт на проверке доступа).
func rolesOf(v any) []auth.Role {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]auth.Role, 0, len(raw))
	for _, r := range raw {
		if s, ok := r.(string); ok {
			out = append(out, auth.Role(s))
		}
	}
	return out
}
