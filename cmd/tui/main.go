package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rolandnsharp/sshmail-server/internal/tui"
)

func getConfig() (host string, port int, keyPath string) {
	host = os.Getenv("SSHMAIL_HOST")
	if host == "" {
		host = "ssh.sshmail.dev"
	}
	port = 2233
	if p := os.Getenv("SSHMAIL_PORT"); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			port = n
		}
	}
	keyPath = os.Getenv("SSHMAIL_KEY")
	if keyPath == "" {
		home, _ := os.UserHomeDir()
		keyPath = filepath.Join(home, ".ssh", "id_ed25519")
	}
	return
}

func main() {
	host, port, keyPath := getConfig()

	// Also support -host, -port, -key flags
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-host":
			if i+1 < len(args) {
				host = args[i+1]
				i++
			}
		case "-port":
			if i+1 < len(args) {
				if n, err := strconv.Atoi(args[i+1]); err == nil {
					port = n
				}
				i++
			}
		case "-key":
			if i+1 < len(args) {
				keyPath = args[i+1]
				i++
			}
		}
	}

	client, err := NewClient(host, port, keyPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// RemoteBackend wraps the SSH client to implement tui.Backend
	backend := &RemoteBackend{client: client}

	p := tea.NewProgram(
		tui.NewModel(backend),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
