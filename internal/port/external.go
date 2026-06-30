package port

import (
	"context"
)

// SecretSource abstracts secret loading (env now, Vault later).
type SecretSource interface {
	Get(ctx context.Context, key string) (string, error)
}

// ASUClient abstracts integration with external АСУ systems.
type ASUClient interface {
	Pull(ctx context.Context, resource string) ([]byte, error)
	Push(ctx context.Context, resource string, payload []byte) error
}
