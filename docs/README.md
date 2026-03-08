# sshmail docs

## Architecture

![Architecture](diagrams/architecture.svg)

## Source Documentation

| Doc | Source File | Description |
|-----|-----------|-------------|
| [hub.md](hub.md) | `cmd/hub/main.go` | Server entry point, startup, shutdown |
| [api.md](api.md) | `internal/api/api.go` | Command dispatch, all handlers, rate limiter |
| [auth.md](auth.md) | `internal/auth/auth.go` | SSH public key authentication |
| [store.md](store.md) | `internal/store/` | Data layer, schema, all CRUD operations |
| [config.md](config.md) | `internal/config/config.go` | Environment configuration |

## Design Documents

| Doc | Status | Description |
|-----|--------|-------------|
| [discovery.md](discovery.md) | Implemented (phase 1+2) | Anonymous sends, recipient controls, discoverability |
| [e2e-encryption.md](e2e-encryption.md) | Design only | End-to-end encryption using SSH keys, signed messages, trust graph |
| [encryption-at-rest.md](encryption-at-rest.md) | Punted | Encryption at rest options (E2E solves this) |

## Diagrams

All diagrams are in `docs/diagrams/` as `.dot` (source), `.svg`, and `.png`.

| Diagram | Description |
|---------|-------------|
| [architecture](diagrams/architecture.svg) | System overview: clients, server, data |
| [discovery-flow](diagrams/discovery-flow.svg) | Anonymous send flow with auth, rate limiting, blocking |
| [e2e-encryption](diagrams/e2e-encryption.svg) | DM encryption: sender → hub → recipient |
| [e2e-room](diagrams/e2e-room.svg) | Room encryption: one message key, N wrapped keys |
| [trust-graph](diagrams/trust-graph.svg) | Invite chain as web of trust |
| [recipient-controls](diagrams/recipient-controls.svg) | Accept/block controls for anonymous messages |
