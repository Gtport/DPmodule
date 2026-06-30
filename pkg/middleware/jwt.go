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
			log.Debug("JWT: no bearer token", zap.Error(err))
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing or malformed Authorization header"})
			return
		}

		claims, err := k.validate(c.Request.Context(), raw)
		if err != nil {
			log.Warn("JWT: validation failed", zap.Error(err))
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}

		c.Request = c.Request.WithContext(auth.WithClaims(c.Request.Context(), claims))
		c.Next()
	}
}

// RequireRole returns middleware that additionally checks Keycloak realm roles.
func (k *KeycloakJWT) RequireRole(roles ...auth.Role) gin.HandlerFunc {
	return func(c *gin.Context) {
		cl := auth.ClaimsFromContext(c.Request.Context())
		if cl == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
			return
		}
		if !cl.HasRole(roles...) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			return
		}
		c.Next()
	}
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
	token, err := jwt.Parse(raw, k.keyFunc(ctx),
		jwt.WithIssuer(k.cfg.Issuer),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, err
	}

	mapClaims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid claims")
	}

	return extractClaims(mapClaims)
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
	if time.Since(k.loadAt) > 5*time.Minute {
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

func (k *KeycloakJWT) fetchJWKS(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, k.cfg.JWKSURL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

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

func extractClaims(m jwt.MapClaims) (*auth.Claims, error) {
	sub, _ := m["sub"].(string)
	email, _ := m["email"].(string)
	username, _ := m["preferred_username"].(string)

	var roles []auth.Role
	if ra, ok := m["realm_access"].(map[string]any); ok {
		if rawRoles, ok := ra["roles"].([]any); ok {
			for _, r := range rawRoles {
				if s, ok := r.(string); ok {
					roles = append(roles, auth.Role(s))
				}
			}
		}
	}

	return &auth.Claims{
		Subject:  sub,
		Email:    email,
		Username: username,
		Roles:    roles,
	}, nil
}
