package llm

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

type sessionIDKey struct{}

// WithSessionID carries the active session id on the context.
func WithSessionID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, sessionIDKey{}, id)
}

// SessionIDFrom extracts the session id from context if present.
func SessionIDFrom(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	id, ok := ctx.Value(sessionIDKey{}).(string)
	return id, ok && id != ""
}

// NewSessionID generates a random 32-character hexadecimal session identifier.
func NewSessionID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
