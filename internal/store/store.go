package store

import "time"

type Agent struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Fingerprint string    `json:"fingerprint,omitempty"`
	PublicKey   string    `json:"-"`
	Bio         string    `json:"bio,omitempty"`
	Email       *string   `json:"email,omitempty"`
	Public      bool      `json:"public,omitempty"`
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

type AgentKey struct {
	Fingerprint string    `json:"fingerprint"`
	PublicKey   string    `json:"public_key"`
	AddedAt     time.Time `json:"added_at"`
}

type GroupMember struct {
	AgentID  int64     `json:"id"`
	Name     string    `json:"name"`
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joined_at"`
}

type Store interface {
	// Agents
	AgentByFingerprint(fingerprint string) (*Agent, error)
	AgentByID(id int64) (*Agent, error)
	AgentByName(name string) (*Agent, error)
	CreateAgent(name, fingerprint, publicKey string, invitedBy int64) (*Agent, error)
	CreateChannel(name, bio string) (*Agent, error)
	UpdateBio(id int64, bio string) error
	UpdateEmail(id int64, email *string) error
	AgentByEmail(email string) (*Agent, error)
	ListAgents() ([]Agent, error)

	// Messages
	SendMessage(fromID, toID int64, body string, fileName, filePath *string) (int64, error)
	Inbox(agentID int64, all bool, after *time.Time) ([]Message, error)
	GetMessage(id int64) (*Message, error)
	MarkRead(id int64) error
	UnreadCount(agentID int64) (int, error)
	UnreadCounts(agentID int64) (map[string]int, error)

	// Keys
	AddKey(agentID int64, fingerprint, publicKey string) error
	RemoveKey(agentID int64, fingerprint string) error
	ListKeys(agentID int64) ([]AgentKey, error)

	// Groups
	CreateGroup(name, bio string, adminID int64) (*Agent, error)
	AddGroupMember(groupID, memberID int64) error
	RemoveGroupMember(groupID, memberID int64) error
	GroupMembers(groupID int64) ([]GroupMember, error)
	GroupRole(groupID, agentID int64) (string, error)
	IsGroupMember(groupID, agentID int64) (bool, error)

	// Invites
	CreateInvite(createdBy int64) (string, error)
	RedeemInvite(code, name, fingerprint, publicKey string) (*Agent, error)

	Close() error
}
