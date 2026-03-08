# internal/config/config.go

## Overview

Environment-based configuration. No config files — everything comes from env vars.

## Config Struct

```go
type Config struct {
    Port       int    // SSH listen port (default 2222)
    DataDir    string // SQLite database and file storage
    HostKeyDir string // SSH host key location
    AdminKey   string // Path to admin's public key file (optional)
}
```

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `2222` | SSH server listen port |
| `DATA_DIR` | `./data` | Directory for hub.db and uploaded files |
| `HOST_KEY_DIR` | `./data` | Directory for SSH host key |
| `ADMIN_KEY` | (empty) | Path to admin public key file for auto-seeding |

## Usage

```go
cfg := config.Load()
```

If `ADMIN_KEY` is set, the server will auto-create an `admin` agent on startup using that public key.
