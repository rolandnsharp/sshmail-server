package auth

import (
	"context"

	"github.com/charmbracelet/ssh"
	gossh "golang.org/x/crypto/ssh"

	"github.com/rolandnsharp/sshmail/internal/store"
)

type contextKey string

const agentKey contextKey = "agent"
const fingerprintKey contextKey = "fingerprint"
const pubkeyKey contextKey = "pubkey"

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

// FingerprintFromContext returns the SSH key fingerprint stored during auth.
func FingerprintFromContext(ctx context.Context) string {
	if fp, ok := ctx.Value(fingerprintKey).(string); ok {
		return fp
	}
	return ""
}

// PubKeyFromContext returns the marshaled public key stored during auth.
func PubKeyFromContext(ctx context.Context) string {
	if pk, ok := ctx.Value(pubkeyKey).(string); ok {
		return pk
	}
	return ""
}

// PublicKeyHandler accepts all keys but only sets agent context for known ones.
// This allows unauthenticated users to connect and redeem invites.
func PublicKeyHandler(s store.Store) func(ctx ssh.Context, key ssh.PublicKey) bool {
	return func(ctx ssh.Context, key ssh.PublicKey) bool {
		fingerprint := gossh.FingerprintSHA256(key)
		ctx.SetValue(fingerprintKey, fingerprint)
		ctx.SetValue(pubkeyKey, string(gossh.MarshalAuthorizedKey(key)))
		agent, _ := s.AgentByFingerprint(fingerprint)
		if agent != nil {
			ctx.SetValue(agentKey, agent)
		}
		return true // always allow connection
	}
}
