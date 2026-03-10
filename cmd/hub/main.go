package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	bm "github.com/charmbracelet/wish/bubbletea"
	lm "github.com/charmbracelet/wish/logging"
	"github.com/charmbracelet/keygen"
	gossh "golang.org/x/crypto/ssh"

	"github.com/rolandnsharp/sshmail-server/internal/api"
	"github.com/rolandnsharp/sshmail-server/internal/auth"
	"github.com/rolandnsharp/sshmail-server/internal/config"
	"github.com/rolandnsharp/sshmail-server/internal/store"
	"github.com/rolandnsharp/sshmail-server/internal/tui"
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

	handler := &api.Handler{Store: db, DataDir: cfg.DataDir, Events: api.NewHub()}

	addr := fmt.Sprintf("0.0.0.0:%d", cfg.Port)
	srv, err := wish.NewServer(
		wish.WithAddress(addr),
		wish.WithHostKeyPath(hostKeyPath),
		wish.WithPublicKeyAuth(auth.PublicKeyHandler(db)),
		wish.WithMiddleware(
			lm.Middleware(),
			func(next ssh.Handler) ssh.Handler {
				return func(sess ssh.Session) {
					cmd := sess.Command()
					_, _, ptyActive := sess.Pty()

					// Interactive session with PTY and no command → serve TUI
					if len(cmd) == 0 && ptyActive {
						agent := auth.AgentFromContext(sess.Context())
						if agent == nil {
							wish.Println(sess, "not authenticated")
							return
						}
						backend := &tui.LocalBackend{
							Store:   db,
							Agent:   agent,
							Events:  handler.Events,
							DataDir: cfg.DataDir,
						}
						m := tui.NewModel(backend)
						opts := bm.MakeOptions(sess)
						opts = append(opts, tea.WithAltScreen())
						p := tea.NewProgram(m, opts...)

						_, windowChanges, _ := sess.Pty()
						ctx, cancel := context.WithCancel(sess.Context())
						go func() {
							for {
								select {
								case <-ctx.Done():
									p.Quit()
									return
								case w := <-windowChanges:
									p.Send(tea.WindowSizeMsg{Width: w.Width, Height: w.Height})
								}
							}
						}()

						if _, err := p.Run(); err != nil {
							log.Printf("TUI error for %s: %v", agent.Name, err)
						}
						p.Kill()
						cancel()
						return
					}

					// Non-interactive: use the command API
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
