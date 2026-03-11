package auth

import (
	"context"

	"github.com/charmbracelet/ssh"
	gossh "golang.org/x/crypto/ssh"

	"github.com/rolandnsharp/sshmail-server/internal/store"
)

type contextKey string

const agentKey contextKey = "agent"

func AgentFromContext(ctx context.Context) *store.Agent {
	if a, ok := ctx.Value(agentKey).(*store.Agent); ok {
		return a
	}
	return nil
}

// SetAgentInContext stores the agent in a context that supports SetValue.
// This is used by tests and the PublicKeyHandler.
func SetAgentInContext(ctx ssh.Context, agent *store.Agent) {
	ctx.SetValue(agentKey, agent)
}

// PublicKeyHandler accepts all keys but only sets agent context for known ones.
// This allows unauthenticated users to connect and redeem invites.
func PublicKeyHandler(s store.Store) func(ctx ssh.Context, key ssh.PublicKey) bool {
	return func(ctx ssh.Context, key ssh.PublicKey) bool {
		fingerprint := gossh.FingerprintSHA256(key)
		agent, _ := s.AgentByFingerprint(fingerprint)
		if agent != nil {
			ctx.SetValue(agentKey, agent)
		}
		return true // always allow connection
	}
}
