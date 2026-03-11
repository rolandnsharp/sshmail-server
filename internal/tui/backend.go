package tui

import "time"

// Message represents a chat message.
type Message struct {
	ID     int64      `json:"id"`
	From   string     `json:"from"`
	To     string     `json:"to"`
	Body   string     `json:"message"`
	File   *string    `json:"file,omitempty"`
	At     time.Time  `json:"at"`
	ReadAt *time.Time `json:"read_at,omitempty"`
}

// Agent represents a user/bot on the platform.
type Agent struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Fingerprint string    `json:"fingerprint,omitempty"`
	Bio         string    `json:"bio,omitempty"`
	Public      bool      `json:"public,omitempty"`
	JoinedAt    time.Time `json:"joined_at"`
	InvitedBy   int64     `json:"invited_by,omitempty"`
}

// WatchEvent is a real-time event from the server.
type WatchEvent struct {
	Type string `json:"type"`
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
	Body string `json:"body,omitempty"`
	ID   int64  `json:"id,omitempty"`
	At   string `json:"at,omitempty"`
}

// PollResult holds unread counts.
type PollResult struct {
	Unread int            `json:"unread"`
	Counts map[string]int `json:"counts,omitempty"`
}

// SendResult is the result of sending a message.
type SendResult struct {
	OK bool  `json:"ok"`
	ID int64 `json:"id"`
}

// Backend is the data source for the TUI. Implemented by both the
// SSH client (remote) and the store (local/server-side).
type Backend interface {
	Whoami() (*Agent, error)
	Agents() ([]Agent, error)
	Inbox(all bool) ([]Message, error)
	Board(name string) ([]Message, error)
	Send(to, message string) (*SendResult, error)
	PollCounts() (*PollResult, error)
	Watch(events chan<- WatchEvent) error
	RepoFiles() ([]string, error)
	ReadFile(name string) (string, error)
	Online() (map[string]bool, error) // agent name → online
}
