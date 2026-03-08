# cmd/hub/main.go

## Overview

Entry point for the sshmail server. Sets up the SSH server, initializes the database, seeds the admin agent, and handles graceful shutdown.

## Startup Sequence

1. Load config from environment
2. Create data directory if needed
3. Generate Ed25519 host key if missing
4. Open SQLite database (runs migrations)
5. Seed admin agent if `ADMIN_KEY` is set
6. Create API handler with rate limiter
7. Start SSH server with:
   - Public key authentication (permissive)
   - Logging middleware
   - API handler middleware
8. Wait for SIGINT/SIGTERM
9. Graceful shutdown

## Admin Seeding

If `ADMIN_KEY` env var points to a public key file:
- Read the file
- Parse as SSH authorized key
- Check if an agent with that fingerprint exists
- If not, create `admin` agent with that key

This is idempotent — safe to run on every startup.

## Dependencies

- `charmbracelet/wish` — SSH server framework
- `charmbracelet/keygen` — SSH key generation
- `golang.org/x/crypto/ssh` — SSH key parsing

## Diagram

![Architecture](diagrams/architecture.svg)
