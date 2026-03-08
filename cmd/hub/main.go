package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	lm "github.com/charmbracelet/wish/logging"
	"github.com/charmbracelet/keygen"
	gossh "golang.org/x/crypto/ssh"

	"github.com/rolandnsharp/sshmail-server/internal/api"
	"github.com/rolandnsharp/sshmail-server/internal/auth"
	"github.com/rolandnsharp/sshmail-server/internal/config"
	"github.com/rolandnsharp/sshmail-server/internal/store"
)

func main() {
	cfg := config.Load()

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		log.Fatalf("create data dir: %v", err)
	}

	// Generate host key if needed
	hostKeyPath := cfg.HostKeyDir + "/hub_ed25519"
	if _, err := os.Stat(hostKeyPath); os.IsNotExist(err) {
		key, err := keygen.New(hostKeyPath, keygen.WithKeyType(keygen.Ed25519))
		if err != nil {
			log.Fatalf("generate host key: %v", err)
		}
		if !key.KeyPairExists() {
			if err := key.WriteKeys(); err != nil {
				log.Fatalf("write host key: %v", err)
			}
		}
	}

	db, err := store.NewSQLite(cfg.DataDir)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer db.Close()

	if cfg.AdminKey != "" {
		seedAdmin(db, cfg.AdminKey)
	}

	handler := api.NewHandler(db, cfg.DataDir)

	addr := fmt.Sprintf("0.0.0.0:%d", cfg.Port)
	srv, err := wish.NewServer(
		wish.WithAddress(addr),
		wish.WithHostKeyPath(hostKeyPath),
		wish.WithPublicKeyAuth(auth.PublicKeyHandler(db)),
		wish.WithMiddleware(
			lm.Middleware(),
			func(next ssh.Handler) ssh.Handler {
				return func(sess ssh.Session) {
					handler.Handle(sess)
				}
			},
		),
	)
	if err != nil {
		log.Fatalf("create server: %v", err)
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	log.Printf("Hub listening on %s", addr)
	go func() {
		if err := srv.ListenAndServe(); err != nil {
			log.Fatalf("server: %v", err)
		}
	}()

	<-done
	log.Println("Shutting down...")
	if err := srv.Shutdown(context.Background()); err != nil {
		log.Fatalf("shutdown: %v", err)
	}
}

func seedAdmin(db store.Store, keyPath string) {
	data, err := os.ReadFile(keyPath)
	if err != nil {
		log.Printf("Warning: could not read admin key %s: %v", keyPath, err)
		return
	}
	pubKeyStr := strings.TrimSpace(string(data))
	pubKey, _, _, _, err := gossh.ParseAuthorizedKey([]byte(pubKeyStr))
	if err != nil {
		log.Printf("Warning: invalid admin key: %v", err)
		return
	}
	fingerprint := gossh.FingerprintSHA256(pubKey)

	existing, _ := db.AgentByFingerprint(fingerprint)
	if existing != nil {
		return
	}

	_, err = db.CreateAgent("admin", fingerprint, pubKeyStr, 0)
	if err != nil {
		log.Printf("Warning: could not seed admin: %v", err)
		return
	}
	log.Println("Admin agent seeded")
}
