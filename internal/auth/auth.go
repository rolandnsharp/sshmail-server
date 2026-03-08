package auth

import (
	"context"

	"github.com/charmbracelet/ssh"
	gossh "golang.org/x/crypto/ssh"

	"github.com/rolandnsharp/sshmail/internal/store"
)

type contextKey string

const agentKey contextKey = "agent"

func AgentFromContext(ctx context.Context) *store.Agent {
	if a, ok := ctx.Value(agentKey).(*store.Agent); ok {
		return a
	}
	return nil
}

func PublicKeyHandler(s store.Store) func(ctx ssh.Context, key ssh.PublicKey) bool {
	return func(ctx ssh.Context, key ssh.PublicKey) bool {
		fingerprint := gossh.FingerprintSHA256(key)
		agent, err := s.AgentByFingerprint(fingerprint)
		if err != nil || agent == nil {
			return false
		}
		ctx.SetValue(agentKey, agent)
		return true
	}
}
