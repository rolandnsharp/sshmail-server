package store

import "time"

type Agent struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Fingerprint string    `json:"fingerprint,omitempty"`
	PublicKey   string    `json:"-"`
	Bio         string    `json:"bio,omitempty"`
	Public      bool      `json:"public,omitempty"`
	Guest       bool      `json:"guest,omitempty"`
	JoinedAt    time.Time `json:"joined_at"`
	InvitedBy   int64     `json:"invited_by,omitempty"`
}

type Message struct {
	ID        int64      `json:"id"`
	FromID    int64      `json:"-"`
	FromName  string     `json:"from"`
	ToID      int64      `json:"-"`
	ToName    string     `json:"to"`
	Body      string     `json:"message"`
	FileName  *string    `json:"file,omitempty"`
	FilePath  *string    `json:"-"`
	CreatedAt time.Time  `json:"at"`
	ReadAt    *time.Time `json:"read_at,omitempty"`
}

type Store interface {
	// Agents
	AgentByFingerprint(fingerprint string) (*Agent, error)
	AgentByID(id int64) (*Agent, error)
	AgentByName(name string) (*Agent, error)
	CreateAgent(name, fingerprint, publicKey string, invitedBy int64) (*Agent, error)
	UpdateBio(id int64, bio string) error
	ListAgents() ([]Agent, error)

	// Guest agents (anonymous senders)
	GetOrCreateGuest(fingerprint string) (*Agent, error)

	// Messages
	SendMessage(fromID, toID int64, body string, fileName, filePath *string) (int64, error)
	Inbox(agentID int64, all bool) ([]Message, error)
	GetMessage(id int64) (*Message, error)
	MarkRead(id int64) error
	UnreadCount(agentID int64) (int, error)

	// Invites
	CreateInvite(createdBy int64) (string, error)
	RedeemInvite(code, name, fingerprint, publicKey string) (*Agent, error)

	Close() error
}
