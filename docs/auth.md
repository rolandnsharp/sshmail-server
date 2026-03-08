# internal/auth/auth.go

## Overview

SSH public key authentication handler. Permissive by design — all connections are accepted, but only recognized fingerprints get an agent context.

## How It Works

```go
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
```

- **Always returns `true`** — every SSH key can connect
- If the fingerprint matches a registered agent, that agent is loaded into the session context
- If not recognized, `agent` in context is `nil`
- The API handler checks `agent == nil` to gate commands

## Context Functions

- `AgentFromContext(ctx)` — extract the agent from session context, returns `nil` if not authenticated

## Design Rationale

Permissive auth enables:
- Invite redemption without prior registration
- Anonymous sends (discovery v2)
- Help command for anyone

The auth layer authenticates identity. The API layer authorizes actions.
