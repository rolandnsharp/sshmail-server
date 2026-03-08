package config

import (
	"os"
	"path/filepath"
	"strconv"
)

type Config struct {
	Port       int
	DataDir    string
	HostKeyDir string
	AdminKey   string // path to admin public key file
}

func Load() Config {
	dataDir := envOr("BBS_DATA_DIR", "data")
	port := 2222
	if p, err := strconv.Atoi(os.Getenv("HUB_PORT")); err == nil && p > 0 {
		port = p
	}
	return Config{
		Port:       port,
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
