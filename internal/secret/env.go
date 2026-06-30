package secret

import (
	"context"
	"fmt"
	"os"
)

// EnvSource implements port.SecretSource by reading OS environment variables.
// Replace or wrap with a Vault implementation without changing callers.
type EnvSource struct{}

func NewEnvSource() *EnvSource { return &EnvSource{} }

func (e *EnvSource) Get(_ context.Context, key string) (string, error) {
	v := os.Getenv(key)
	if v == "" {
		return "", fmt.Errorf("secret %q not found in environment", key)
	}
	return v, nil
}
