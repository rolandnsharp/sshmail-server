package config

import (
	"os"
	"path/filepath"
)

type Config struct {
	Port       int
	DataDir    string
	HostKeyDir string
	AdminKey   string // path to admin public key file
}

func Load() Config {
	dataDir := envOr("BBS_DATA_DIR", "data")
	return Config{
		Port:       2222,
		DataDir:    dataDir,
		HostKeyDir: filepath.Join(dataDir),
		AdminKey:   os.Getenv("BBS_ADMIN_KEY"),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
