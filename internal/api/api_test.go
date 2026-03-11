package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/ssh"

	"github.com/rolandnsharp/sshmail-server/internal/auth"
	"github.com/rolandnsharp/sshmail-server/internal/store"
)

// ---------------------------------------------------------------------------
// Mock store
// ---------------------------------------------------------------------------

type mockStore struct {
	mu       sync.Mutex
	agents   map[int64]*store.Agent
	messages map[int64]*store.Message
	keys     map[int64][]store.AgentKey
	groups   map[int64]map[int64]string // groupID -> memberID -> role
	invites  map[string]int64           // code -> createdBy
	nextID   int64
}

func newMockStore() *mockStore {
	return &mockStore{
		agents:   make(map[int64]*store.Agent),
		messages: make(map[int64]*store.Message),
		keys:     make(map[int64][]store.AgentKey),
		groups:   make(map[int64]map[int64]string),
		invites:  make(map[string]int64),
		nextID:   1,
	}
}

func (s *mockStore) allocID() int64 {
	id := s.nextID
	s.nextID++
	return id
}

func (s *mockStore) AgentByFingerprint(fp string) (*store.Agent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.agents {
		if a.Fingerprint == fp {
			return a, nil
		}
	}
	return nil, nil
}

func (s *mockStore) AgentByID(id int64) (*store.Agent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.agents[id]
	if !ok {
		return nil, nil
	}
	return a, nil
}

func (s *mockStore) AgentByName(name string) (*store.Agent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.agents {
		if a.Name == name {
			return a, nil
		}
	}
	return nil, nil
}

func (s *mockStore) CreateAgent(name, fp, pubKey string, invitedBy int64) (*store.Agent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := &store.Agent{
		ID:          s.allocID(),
		Name:        name,
		Fingerprint: fp,
		PublicKey:   pubKey,
		JoinedAt:    time.Now(),
		InvitedBy:   invitedBy,
	}
	s.agents[a.ID] = a
	return a, nil
}

func (s *mockStore) CreateChannel(name, bio string) (*store.Agent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := &store.Agent{
		ID:       s.allocID(),
		Name:     name,
		Bio:      bio,
		Public:   true,
		JoinedAt: time.Now(),
	}
	s.agents[a.ID] = a
	return a, nil
}

func (s *mockStore) UpdateBio(id int64, bio string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.agents[id]
	if !ok {
		return fmt.Errorf("agent not found")
	}
	a.Bio = bio
	return nil
}

func (s *mockStore) UpdateEmail(id int64, email *string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.agents[id]
	if !ok {
		return fmt.Errorf("agent not found")
	}
	a.Email = email
	return nil
}

func (s *mockStore) AgentByEmail(email string) (*store.Agent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.agents {
		if a.Email != nil && *a.Email == email {
			return a, nil
		}
	}
	return nil, nil
}

func (s *mockStore) ListAgents() ([]store.Agent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.Agent
	for _, a := range s.agents {
		out = append(out, *a)
	}
	return out, nil
}

func (s *mockStore) SendMessage(fromID, toID int64, body string, fileName, filePath *string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.allocID()
	from := s.agents[fromID]
	to := s.agents[toID]
	var fromName, toName string
	if from != nil {
		fromName = from.Name
	}
	if to != nil {
		toName = to.Name
	}
	s.messages[id] = &store.Message{
		ID:        id,
		FromID:    fromID,
		FromName:  fromName,
		ToID:      toID,
		ToName:    toName,
		Body:      body,
		FileName:  fileName,
		FilePath:  filePath,
		CreatedAt: time.Now(),
	}
	return id, nil
}

func (s *mockStore) Inbox(agentID int64, all bool, after *time.Time) ([]store.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.Message
	for _, m := range s.messages {
		if m.ToID == agentID {
			if !all && m.ReadAt != nil {
				continue
			}
			if after != nil && m.CreatedAt.Before(*after) {
				continue
			}
			out = append(out, *m)
		}
	}
	return out, nil
}

func (s *mockStore) GetMessage(id int64) (*store.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.messages[id]
	if !ok {
		return nil, nil
	}
	return m, nil
}

func (s *mockStore) MarkRead(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.messages[id]
	if !ok {
		return fmt.Errorf("not found")
	}
	now := time.Now()
	m.ReadAt = &now
	return nil
}

func (s *mockStore) UnreadCount(agentID int64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, m := range s.messages {
		if m.ToID == agentID && m.ReadAt == nil {
			count++
		}
	}
	return count, nil
}

func (s *mockStore) UnreadCounts(agentID int64) (map[string]int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	counts := make(map[string]int)
	for _, m := range s.messages {
		if m.ToID == agentID && m.ReadAt == nil {
			counts[m.FromName]++
		}
	}
	return counts, nil
}

func (s *mockStore) AddKey(agentID int64, fp, pubKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys[agentID] = append(s.keys[agentID], store.AgentKey{
		Fingerprint: fp,
		PublicKey:   pubKey,
		AddedAt:     time.Now(),
	})
	return nil
}

func (s *mockStore) RemoveKey(agentID int64, fp string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := s.keys[agentID]
	for i, k := range keys {
		if k.Fingerprint == fp {
			s.keys[agentID] = append(keys[:i], keys[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("key not found")
}

func (s *mockStore) ListKeys(agentID int64) ([]store.AgentKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.keys[agentID], nil
}

func (s *mockStore) CreateGroup(name, bio string, adminID int64) (*store.Agent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := &store.Agent{
		ID:       s.allocID(),
		Name:     name,
		Bio:      bio,
		Public:   false,
		JoinedAt: time.Now(),
		// Groups have no PublicKey (PublicKey == "")
	}
	s.agents[a.ID] = a
	s.groups[a.ID] = map[int64]string{adminID: "admin"}
	return a, nil
}

func (s *mockStore) AddGroupMember(groupID, memberID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.groups[groupID] == nil {
		s.groups[groupID] = make(map[int64]string)
	}
	s.groups[groupID][memberID] = "member"
	return nil
}

func (s *mockStore) RemoveGroupMember(groupID, memberID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.groups[groupID]; !ok {
		return fmt.Errorf("group not found")
	}
	delete(s.groups[groupID], memberID)
	return nil
}

func (s *mockStore) GroupMembers(groupID int64) ([]store.GroupMember, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	members, ok := s.groups[groupID]
	if !ok {
		return nil, nil
	}
	var out []store.GroupMember
	for id, role := range members {
		name := ""
		if a, ok := s.agents[id]; ok {
			name = a.Name
		}
		out = append(out, store.GroupMember{
			AgentID:  id,
			Name:     name,
			Role:     role,
			JoinedAt: time.Now(),
		})
	}
	return out, nil
}

func (s *mockStore) GroupRole(groupID, agentID int64) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	members, ok := s.groups[groupID]
	if !ok {
		return "", fmt.Errorf("group not found")
	}
	role, ok := members[agentID]
	if !ok {
		return "", fmt.Errorf("not a member")
	}
	return role, nil
}

func (s *mockStore) IsGroupMember(groupID, agentID int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	members, ok := s.groups[groupID]
	if !ok {
		return false, nil
	}
	_, ok = members[agentID]
	return ok, nil
}

func (s *mockStore) CreateInvite(createdBy int64) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	code := fmt.Sprintf("invite-%d", s.allocID())
	s.invites[code] = createdBy
	return code, nil
}

func (s *mockStore) RedeemInvite(code, name, fp, pubKey string) (*store.Agent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	invitedBy, ok := s.invites[code]
	if !ok {
		return nil, fmt.Errorf("invalid invite code")
	}
	delete(s.invites, code)
	a := &store.Agent{
		ID:          s.allocID(),
		Name:        name,
		Fingerprint: fp,
		PublicKey:   pubKey,
		JoinedAt:    time.Now(),
		InvitedBy:   invitedBy,
	}
	s.agents[a.ID] = a
	return a, nil
}

func (s *mockStore) Close() error { return nil }

// ---------------------------------------------------------------------------
// Mock SSH context
// ---------------------------------------------------------------------------

type mockContext struct {
	context.Context
	mu     sync.Mutex
	values map[interface{}]interface{}
}

func newMockContext(agent *store.Agent) *mockContext {
	ctx := &mockContext{
		Context: context.Background(),
		values:  make(map[interface{}]interface{}),
	}
	if agent != nil {
		auth.SetAgentInContext(ctx, agent)
	}
	return ctx
}

func (c *mockContext) SetValue(key, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values[key] = value
}

func (c *mockContext) Value(key interface{}) interface{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	if v, ok := c.values[key]; ok {
		return v
	}
	return c.Context.Value(key)
}

func (c *mockContext) Lock()   {}
func (c *mockContext) Unlock() {}

// Implement ssh.Context
func (c *mockContext) User() string                     { return "" }
func (c *mockContext) SessionID() string                { return "test-session" }
func (c *mockContext) ClientVersion() string            { return "SSH-2.0-test" }
func (c *mockContext) ServerVersion() string            { return "SSH-2.0-test" }
func (c *mockContext) RemoteAddr() net.Addr             { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345} }
func (c *mockContext) LocalAddr() net.Addr              { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 2222} }
func (c *mockContext) Permissions() *ssh.Permissions     { return &ssh.Permissions{} }

// ---------------------------------------------------------------------------
// Mock SSH session
// ---------------------------------------------------------------------------

type mockSession struct {
	cmd    []string
	ctx    *mockContext
	output bytes.Buffer
	stderr bytes.Buffer
	stdin  io.Reader
}

func newMockSession(agent *store.Agent, cmd []string) *mockSession {
	return &mockSession{
		cmd:   cmd,
		ctx:   newMockContext(agent),
		stdin: &bytes.Buffer{},
	}
}

func newMockSessionWithStdin(agent *store.Agent, cmd []string, stdin io.Reader) *mockSession {
	return &mockSession{
		cmd:   cmd,
		ctx:   newMockContext(agent),
		stdin: stdin,
	}
}

// io.Reader — reads from stdin
func (s *mockSession) Read(p []byte) (int, error)  { return s.stdin.Read(p) }

// io.Writer — writes to output
func (s *mockSession) Write(p []byte) (int, error) { return s.output.Write(p) }
func (s *mockSession) Close() error                { return nil }
func (s *mockSession) CloseWrite() error           { return nil }

func (s *mockSession) SendRequest(name string, wantReply bool, payload []byte) (bool, error) {
	return false, nil
}
func (s *mockSession) Stderr() io.ReadWriter { return &s.stderr }

func (s *mockSession) User() string           { return "testuser" }
func (s *mockSession) RemoteAddr() net.Addr   { return s.ctx.RemoteAddr() }
func (s *mockSession) LocalAddr() net.Addr    { return s.ctx.LocalAddr() }
func (s *mockSession) Environ() []string      { return nil }
func (s *mockSession) Exit(code int) error    { return nil }
func (s *mockSession) Command() []string      { return s.cmd }
func (s *mockSession) RawCommand() string {
	if len(s.cmd) == 0 {
		return ""
	}
	result := ""
	for i, c := range s.cmd {
		if i > 0 {
			result += " "
		}
		result += c
	}
	return result
}
func (s *mockSession) Subsystem() string               { return "" }
func (s *mockSession) PublicKey() ssh.PublicKey          { return nil }
func (s *mockSession) Context() ssh.Context             { return s.ctx }
func (s *mockSession) Permissions() ssh.Permissions      { return ssh.Permissions{} }
func (s *mockSession) EmulatedPty() bool                { return false }
func (s *mockSession) Pty() (ssh.Pty, <-chan ssh.Window, bool) {
	return ssh.Pty{}, nil, false
}
func (s *mockSession) Signals(ch chan<- ssh.Signal)      {}
func (s *mockSession) Break(ch chan<- bool)              {}

func (s *mockSession) outputJSON() map[string]interface{} {
	var result map[string]interface{}
	json.Unmarshal(s.output.Bytes(), &result)
	return result
}

// ---------------------------------------------------------------------------
// Test helper
// ---------------------------------------------------------------------------

func setupHandler(t *testing.T) (*Handler, *mockStore) {
	t.Helper()
	ms := newMockStore()
	h := &Handler{
		Store:   ms,
		DataDir: t.TempDir(),
		Events:  NewHub(),
	}
	return h, ms
}

func addAgent(ms *mockStore, name string) *store.Agent {
	a, _ := ms.CreateAgent(name, "fp-"+name, "ssh-ed25519 AAAA "+name, 0)
	return a
}

func assertError(t *testing.T, out map[string]interface{}, substr string) {
	t.Helper()
	errVal, ok := out["error"]
	if !ok {
		t.Fatalf("expected error containing %q, got response: %v", substr, out)
	}
	errStr, ok := errVal.(string)
	if !ok {
		t.Fatalf("expected error to be string, got %T", errVal)
	}
	if substr != "" {
		found := false
		for _, s := range []string{substr} {
			if contains(errStr, s) {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected error containing %q, got: %s", substr, errStr)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (substr == "" || bytes.Contains([]byte(s), []byte(substr)))
}

func assertOK(t *testing.T, out map[string]interface{}) {
	t.Helper()
	if _, hasErr := out["error"]; hasErr {
		t.Fatalf("expected ok, got error: %v", out["error"])
	}
}

func assertNoError(t *testing.T, out map[string]interface{}) {
	t.Helper()
	if e, ok := out["error"]; ok {
		t.Fatalf("unexpected error: %v", e)
	}
}

// ---------------------------------------------------------------------------
// Auth boundary tests
// ---------------------------------------------------------------------------

func TestUnauthenticatedCommands(t *testing.T) {
	h, _ := setupHandler(t)

	// Commands that should fail for unauthenticated users.
	authRequired := []struct {
		name string
		cmd  []string
	}{
		{"inbox", []string{"inbox"}},
		{"send", []string{"send", "alice", "hello"}},
		{"read", []string{"read", "1"}},
		{"agents", []string{"agents"}},
		{"bio", []string{"bio", "hello"}},
		{"email", []string{"email"}},
		{"keys", []string{"keys"}},
		{"addkey", []string{"addkey"}},
		{"group", []string{"group", "create", "mygroup"}},
		{"channel", []string{"channel", "general"}},
		{"board", []string{"board"}},
		{"poll", []string{"poll"}},
		{"fetch", []string{"fetch", "1"}},
		{"whoami", []string{"whoami"}},
		{"invite_create", []string{"invite"}},
	}

	for _, tc := range authRequired {
		t.Run(tc.name, func(t *testing.T) {
			sess := newMockSession(nil, tc.cmd)
			h.Handle(sess)
			out := sess.outputJSON()
			assertError(t, out, "not authenticated")
		})
	}
}

func TestUnauthenticatedHelpAllowed(t *testing.T) {
	h, _ := setupHandler(t)

	// Empty command → help, allowed without auth.
	sess := newMockSession(nil, nil)
	h.Handle(sess)
	out := sess.outputJSON()
	if _, ok := out["commands"]; !ok {
		t.Fatal("expected help output with commands key")
	}
	if _, ok := out["error"]; ok {
		t.Fatal("help should not return error for unauthenticated user")
	}
}

func TestUnauthenticatedInviteRedeemAllowed(t *testing.T) {
	h, ms := setupHandler(t)

	// First create an invite via the store directly.
	admin := addAgent(ms, "admin")
	code, _ := ms.CreateInvite(admin.ID)

	// Redeem invite: invite <code> <name> — requires 3+ args, allowed without auth.
	// Needs a valid public key on stdin.
	pubKeyStr := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIHGhRjpUamBGjCdHNvRLqRgKYRBNkXMaZ0sCSqIN7poN test@test\n"
	sess := newMockSessionWithStdin(nil, []string{"invite", code, "newuser"}, bytes.NewBufferString(pubKeyStr))
	h.Handle(sess)
	out := sess.outputJSON()
	assertNoError(t, out)
	if out["name"] != "newuser" {
		t.Fatalf("expected name=newuser, got %v", out["name"])
	}
}

func TestUnauthenticatedInviteCreateDenied(t *testing.T) {
	h, _ := setupHandler(t)

	// invite with <3 args requires auth (it's invite create)
	sess := newMockSession(nil, []string{"invite"})
	h.Handle(sess)
	out := sess.outputJSON()
	assertError(t, out, "not authenticated")

	sess2 := newMockSession(nil, []string{"invite", "onlycode"})
	h.Handle(sess2)
	out2 := sess2.outputJSON()
	assertError(t, out2, "not authenticated")
}

// ---------------------------------------------------------------------------
// Message access control tests
// ---------------------------------------------------------------------------

func TestDMAccessControl(t *testing.T) {
	h, ms := setupHandler(t)

	alice := addAgent(ms, "alice")
	bob := addAgent(ms, "bob")
	charlie := addAgent(ms, "charlie")

	// Alice sends a DM to Bob.
	sessSend := newMockSession(alice, []string{"send", "bob", "secret", "message"})
	h.Handle(sessSend)
	sendOut := sessSend.outputJSON()
	assertOK(t, sendOut)
	msgID := sendOut["id"] // float64

	idStr := fmt.Sprintf("%d", int64(msgID.(float64)))

	t.Run("recipient can read", func(t *testing.T) {
		sess := newMockSession(bob, []string{"read", idStr})
		h.Handle(sess)
		out := sess.outputJSON()
		assertNoError(t, out)
		if out["message"] != "secret message" {
			t.Fatalf("expected message body, got %v", out["message"])
		}
	})

	t.Run("sender can read", func(t *testing.T) {
		sess := newMockSession(alice, []string{"read", idStr})
		h.Handle(sess)
		out := sess.outputJSON()
		assertNoError(t, out)
		if out["message"] != "secret message" {
			t.Fatalf("expected message body, got %v", out["message"])
		}
	})

	t.Run("third party cannot read", func(t *testing.T) {
		sess := newMockSession(charlie, []string{"read", idStr})
		h.Handle(sess)
		out := sess.outputJSON()
		assertError(t, out, "message not found")
	})
}

func TestReadInvalidID(t *testing.T) {
	h, ms := setupHandler(t)
	alice := addAgent(ms, "alice")

	t.Run("non-numeric ID", func(t *testing.T) {
		sess := newMockSession(alice, []string{"read", "abc"})
		h.Handle(sess)
		out := sess.outputJSON()
		assertError(t, out, "invalid message id")
	})

	t.Run("nonexistent ID", func(t *testing.T) {
		sess := newMockSession(alice, []string{"read", "99999"})
		h.Handle(sess)
		out := sess.outputJSON()
		assertError(t, out, "message not found")
	})

	t.Run("no args", func(t *testing.T) {
		sess := newMockSession(alice, []string{"read"})
		h.Handle(sess)
		out := sess.outputJSON()
		assertError(t, out, "usage")
	})
}

func TestFetchAccessControl(t *testing.T) {
	h, ms := setupHandler(t)

	alice := addAgent(ms, "alice")
	bob := addAgent(ms, "bob")
	charlie := addAgent(ms, "charlie")

	// Send DM from alice to bob.
	sessSend := newMockSession(alice, []string{"send", "bob", "file-message"})
	h.Handle(sessSend)
	sendOut := sessSend.outputJSON()
	idStr := fmt.Sprintf("%d", int64(sendOut["id"].(float64)))

	t.Run("recipient can fetch", func(t *testing.T) {
		sess := newMockSession(bob, []string{"fetch", idStr})
		h.Handle(sess)
		out := sess.outputJSON()
		assertNoError(t, out)
	})

	t.Run("sender can fetch", func(t *testing.T) {
		sess := newMockSession(alice, []string{"fetch", idStr})
		h.Handle(sess)
		out := sess.outputJSON()
		assertNoError(t, out)
	})

	t.Run("third party cannot fetch", func(t *testing.T) {
		sess := newMockSession(charlie, []string{"fetch", idStr})
		h.Handle(sess)
		out := sess.outputJSON()
		assertError(t, out, "message not found")
	})

	t.Run("invalid id", func(t *testing.T) {
		sess := newMockSession(alice, []string{"fetch", "abc"})
		h.Handle(sess)
		out := sess.outputJSON()
		assertError(t, out, "invalid message id")
	})
}

// ---------------------------------------------------------------------------
// Group access control tests
// ---------------------------------------------------------------------------

func TestGroupSendAccessControl(t *testing.T) {
	h, ms := setupHandler(t)

	alice := addAgent(ms, "alice")
	bob := addAgent(ms, "bob")
	charlie := addAgent(ms, "charlie")

	// Alice creates a private group.
	sessCreate := newMockSession(alice, []string{"group", "create", "secret-club"})
	h.Handle(sessCreate)
	createOut := sessCreate.outputJSON()
	assertOK(t, createOut)

	// Add bob to the group.
	sessAdd := newMockSession(alice, []string{"group", "add", "secret-club", "bob"})
	h.Handle(sessAdd)
	assertOK(t, sessAdd.outputJSON())

	t.Run("member can send to group", func(t *testing.T) {
		sess := newMockSession(bob, []string{"send", "secret-club", "hello", "group"})
		h.Handle(sess)
		out := sess.outputJSON()
		assertOK(t, out)
	})

	t.Run("non-member cannot send to group", func(t *testing.T) {
		sess := newMockSession(charlie, []string{"send", "secret-club", "sneaky"})
		h.Handle(sess)
		out := sess.outputJSON()
		assertError(t, out, "not a member")
	})
}

func TestGroupAddMember(t *testing.T) {
	h, ms := setupHandler(t)

	alice := addAgent(ms, "alice")
	bob := addAgent(ms, "bob")
	addAgent(ms, "charlie") // will be added by name
	dave := addAgent(ms, "dave")

	// Alice creates group.
	sess := newMockSession(alice, []string{"group", "create", "team"})
	h.Handle(sess)
	assertOK(t, sess.outputJSON())

	// Alice adds bob.
	sess2 := newMockSession(alice, []string{"group", "add", "team", "bob"})
	h.Handle(sess2)
	assertOK(t, sess2.outputJSON())

	t.Run("member (non-admin) can add another member", func(t *testing.T) {
		sess := newMockSession(bob, []string{"group", "add", "team", "charlie"})
		h.Handle(sess)
		assertOK(t, sess.outputJSON())
	})

	t.Run("non-member cannot add", func(t *testing.T) {
		sess := newMockSession(dave, []string{"group", "add", "team", "dave"})
		h.Handle(sess)
		out := sess.outputJSON()
		assertError(t, out, "not a member")
	})
}

func TestGroupRemoveMember(t *testing.T) {
	h, ms := setupHandler(t)

	alice := addAgent(ms, "alice")
	bob := addAgent(ms, "bob")
	charlie := addAgent(ms, "charlie")

	// Alice creates group and adds bob and charlie.
	sess := newMockSession(alice, []string{"group", "create", "team"})
	h.Handle(sess)
	assertOK(t, sess.outputJSON())

	for _, name := range []string{"bob", "charlie"} {
		s := newMockSession(alice, []string{"group", "add", "team", name})
		h.Handle(s)
		assertOK(t, s.outputJSON())
	}

	t.Run("non-admin cannot remove another member", func(t *testing.T) {
		sess := newMockSession(bob, []string{"group", "remove", "team", "charlie"})
		h.Handle(sess)
		out := sess.outputJSON()
		assertError(t, out, "only the group admin")
	})

	t.Run("member can remove themselves", func(t *testing.T) {
		sess := newMockSession(charlie, []string{"group", "remove", "team", "charlie"})
		h.Handle(sess)
		assertOK(t, sess.outputJSON())
	})

	t.Run("admin can remove a member", func(t *testing.T) {
		sess := newMockSession(alice, []string{"group", "remove", "team", "bob"})
		h.Handle(sess)
		assertOK(t, sess.outputJSON())
	})

	t.Run("admin cannot remove themselves", func(t *testing.T) {
		sess := newMockSession(alice, []string{"group", "remove", "team", "alice"})
		h.Handle(sess)
		assertError(t, sess.outputJSON(), "admin cannot leave")
	})
}

func TestInviteNameValidation(t *testing.T) {
	h, _ := setupHandler(t)

	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{"empty name", "", "1-32 characters"},
		{"too long", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "1-32 characters"},
		{"uppercase", "Alice", "lowercase"},
		{"spaces", "bob smith", "lowercase"},
		{"special chars", "bob@evil", "lowercase"},
		{"path traversal", "../etc", "lowercase"},
		{"valid", "good-name_1", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sess := newMockSession(nil, []string{"invite", "fakecode", tt.input})
			h.Handle(sess)
			out := sess.outputJSON()
			if tt.wantErr != "" {
				assertError(t, out, tt.wantErr)
			}
		})
	}
}

func TestGroupMembersAccess(t *testing.T) {
	h, ms := setupHandler(t)

	alice := addAgent(ms, "alice")
	bob := addAgent(ms, "bob")
	charlie := addAgent(ms, "charlie")

	sess := newMockSession(alice, []string{"group", "create", "secret"})
	h.Handle(sess)
	assertOK(t, sess.outputJSON())

	sess2 := newMockSession(alice, []string{"group", "add", "secret", "bob"})
	h.Handle(sess2)
	assertOK(t, sess2.outputJSON())

	t.Run("member can list members", func(t *testing.T) {
		sess := newMockSession(bob, []string{"group", "members", "secret"})
		h.Handle(sess)
		out := sess.outputJSON()
		assertNoError(t, out)
		if out["group"] != "secret" {
			t.Fatalf("expected group=secret, got %v", out["group"])
		}
	})

	t.Run("non-member cannot list members", func(t *testing.T) {
		sess := newMockSession(charlie, []string{"group", "members", "secret"})
		h.Handle(sess)
		out := sess.outputJSON()
		assertError(t, out, "not a member")
	})
}

// ---------------------------------------------------------------------------
// Email uniqueness tests
// ---------------------------------------------------------------------------

func TestEmailSetAndClear(t *testing.T) {
	h, ms := setupHandler(t)
	alice := addAgent(ms, "alice")

	t.Run("set email", func(t *testing.T) {
		sess := newMockSession(alice, []string{"email", "alice@example.com"})
		h.Handle(sess)
		out := sess.outputJSON()
		assertOK(t, out)
		if out["email"] != "alice@example.com" {
			t.Fatalf("expected email=alice@example.com, got %v", out["email"])
		}
	})

	t.Run("show email", func(t *testing.T) {
		sess := newMockSession(alice, []string{"email"})
		h.Handle(sess)
		out := sess.outputJSON()
		assertNoError(t, out)
		if out["email"] != "alice@example.com" {
			t.Fatalf("expected alice@example.com, got %v", out["email"])
		}
	})

	t.Run("clear email", func(t *testing.T) {
		sess := newMockSession(alice, []string{"email", "clear"})
		h.Handle(sess)
		out := sess.outputJSON()
		assertOK(t, out)
	})

	t.Run("email cleared is nil", func(t *testing.T) {
		sess := newMockSession(alice, []string{"email"})
		h.Handle(sess)
		out := sess.outputJSON()
		assertNoError(t, out)
		// After clear, email should be nil/null.
		if out["email"] != nil {
			t.Fatalf("expected email=nil after clear, got %v", out["email"])
		}
	})
}

func TestEmailUniqueness(t *testing.T) {
	h, ms := setupHandler(t)
	alice := addAgent(ms, "alice")
	bob := addAgent(ms, "bob")

	// Alice sets email.
	sess := newMockSession(alice, []string{"email", "shared@example.com"})
	h.Handle(sess)
	assertOK(t, sess.outputJSON())

	// Bob tries the same email.
	sess2 := newMockSession(bob, []string{"email", "shared@example.com"})
	h.Handle(sess2)
	out := sess2.outputJSON()
	assertError(t, out, "already in use")
}

func TestEmailInvalid(t *testing.T) {
	h, ms := setupHandler(t)
	alice := addAgent(ms, "alice")

	sess := newMockSession(alice, []string{"email", "notanemail"})
	h.Handle(sess)
	out := sess.outputJSON()
	assertError(t, out, "invalid email")
}

// ---------------------------------------------------------------------------
// Input validation tests
// ---------------------------------------------------------------------------

func TestSendNoArgs(t *testing.T) {
	h, ms := setupHandler(t)
	alice := addAgent(ms, "alice")

	sess := newMockSession(alice, []string{"send"})
	h.Handle(sess)
	out := sess.outputJSON()
	assertError(t, out, "usage")
}

func TestSendOneArg(t *testing.T) {
	h, ms := setupHandler(t)
	alice := addAgent(ms, "alice")

	sess := newMockSession(alice, []string{"send", "bob"})
	h.Handle(sess)
	out := sess.outputJSON()
	assertError(t, out, "usage")
}

func TestSanitizeRepoName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"/alice.git", "alice"},
		{"../../etc/passwd", "passwd"},
		{"../../../root/.ssh/authorized_keys.git", "authorized_keys"},
		{"/foo/bar/baz.git", "baz"},
		{"alice", "alice"},
		{"/alice", "alice"},
		{"alice.git", "alice"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := sanitizeRepoName(tc.input)
			if got != tc.expected {
				t.Fatalf("sanitizeRepoName(%q) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Unknown command test
// ---------------------------------------------------------------------------

func TestUnknownCommand(t *testing.T) {
	h, ms := setupHandler(t)
	alice := addAgent(ms, "alice")

	sess := newMockSession(alice, []string{"nosuchcmd"})
	h.Handle(sess)
	out := sess.outputJSON()
	assertError(t, out, "unknown command")
}

// ---------------------------------------------------------------------------
// Group message read access — member can read, non-member cannot
// ---------------------------------------------------------------------------

func TestGroupMessageReadAccess(t *testing.T) {
	h, ms := setupHandler(t)

	alice := addAgent(ms, "alice")
	bob := addAgent(ms, "bob")
	outsider := addAgent(ms, "outsider")

	// Alice creates group and adds bob.
	sessCreate := newMockSession(alice, []string{"group", "create", "devteam"})
	h.Handle(sessCreate)
	assertOK(t, sessCreate.outputJSON())

	sessAdd := newMockSession(alice, []string{"group", "add", "devteam", "bob"})
	h.Handle(sessAdd)
	assertOK(t, sessAdd.outputJSON())

	// Alice sends to group.
	sessSend := newMockSession(alice, []string{"send", "devteam", "group-secret"})
	h.Handle(sessSend)
	sendOut := sessSend.outputJSON()
	assertOK(t, sendOut)
	idStr := fmt.Sprintf("%d", int64(sendOut["id"].(float64)))

	t.Run("group member can read", func(t *testing.T) {
		sess := newMockSession(bob, []string{"read", idStr})
		h.Handle(sess)
		out := sess.outputJSON()
		assertNoError(t, out)
	})

	t.Run("non-member cannot read group message", func(t *testing.T) {
		sess := newMockSession(outsider, []string{"read", idStr})
		h.Handle(sess)
		out := sess.outputJSON()
		assertError(t, out, "message not found")
	})
}

// ---------------------------------------------------------------------------
// Auth context integration — verify AgentFromContext works
// ---------------------------------------------------------------------------

func TestAgentFromContextNilWhenNotSet(t *testing.T) {
	ctx := newMockContext(nil)
	agent := auth.AgentFromContext(ctx)
	if agent != nil {
		t.Fatal("expected nil agent for unauthenticated context")
	}
}

func TestAgentFromContextReturnsAgent(t *testing.T) {
	expected := &store.Agent{ID: 42, Name: "test"}
	ctx := newMockContext(expected)
	agent := auth.AgentFromContext(ctx)
	if agent == nil {
		t.Fatal("expected non-nil agent")
	}
	if agent.ID != 42 || agent.Name != "test" {
		t.Fatalf("agent mismatch: got %+v", agent)
	}
}

// ---------------------------------------------------------------------------
// Send to nonexistent agent
// ---------------------------------------------------------------------------

func TestSendToNonexistentAgent(t *testing.T) {
	h, ms := setupHandler(t)
	alice := addAgent(ms, "alice")

	sess := newMockSession(alice, []string{"send", "nobody", "hello"})
	h.Handle(sess)
	out := sess.outputJSON()
	assertError(t, out, "agent not found")
}

// ---------------------------------------------------------------------------
// Board — non-public agent cannot be viewed as board
// ---------------------------------------------------------------------------

func TestBoardNonPublic(t *testing.T) {
	h, ms := setupHandler(t)
	alice := addAgent(ms, "alice")
	addAgent(ms, "bob") // regular agent, not public

	sess := newMockSession(alice, []string{"board", "bob"})
	h.Handle(sess)
	out := sess.outputJSON()
	assertError(t, out, "not a public board")
}

func TestBoardPublicChannel(t *testing.T) {
	h, ms := setupHandler(t)
	alice := addAgent(ms, "alice")

	// Create a public channel.
	sessCreate := newMockSession(alice, []string{"channel", "announcements", "Public", "announcements"})
	h.Handle(sessCreate)
	assertOK(t, sessCreate.outputJSON())

	sess := newMockSession(alice, []string{"board", "announcements"})
	h.Handle(sess)
	out := sess.outputJSON()
	assertNoError(t, out)
	if out["board"] != "announcements" {
		t.Fatalf("expected board=announcements, got %v", out["board"])
	}
}

// ---------------------------------------------------------------------------
// Email — setting same email by same agent is OK
// ---------------------------------------------------------------------------

func TestEmailSameAgentCanResetOwn(t *testing.T) {
	h, ms := setupHandler(t)
	alice := addAgent(ms, "alice")

	sess := newMockSession(alice, []string{"email", "alice@example.com"})
	h.Handle(sess)
	assertOK(t, sess.outputJSON())

	// Setting the same email again for the same agent should work.
	sess2 := newMockSession(alice, []string{"email", "alice@example.com"})
	h.Handle(sess2)
	assertOK(t, sess2.outputJSON())
}

// ---------------------------------------------------------------------------
// Whoami returns agent info
// ---------------------------------------------------------------------------

func TestWhoami(t *testing.T) {
	h, ms := setupHandler(t)
	alice := addAgent(ms, "alice")

	sess := newMockSession(alice, []string{"whoami"})
	h.Handle(sess)
	out := sess.outputJSON()
	assertNoError(t, out)
	if out["name"] != "alice" {
		t.Fatalf("expected name=alice, got %v", out["name"])
	}
}

// ---------------------------------------------------------------------------
// Poll returns unread count
// ---------------------------------------------------------------------------

func TestPoll(t *testing.T) {
	h, ms := setupHandler(t)
	alice := addAgent(ms, "alice")
	bob := addAgent(ms, "bob")

	// Send a message to alice.
	sessSend := newMockSession(bob, []string{"send", "alice", "hey"})
	h.Handle(sessSend)
	assertOK(t, sessSend.outputJSON())

	sess := newMockSession(alice, []string{"poll"})
	h.Handle(sess)
	out := sess.outputJSON()
	assertNoError(t, out)
	unread, ok := out["unread"].(float64)
	if !ok || unread != 1 {
		t.Fatalf("expected unread=1, got %v", out["unread"])
	}
}
