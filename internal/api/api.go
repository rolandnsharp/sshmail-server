package api

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/ssh"
	"github.com/google/uuid"
	gossh "golang.org/x/crypto/ssh"

	"github.com/rolandnsharp/sshmail/internal/auth"
	"github.com/rolandnsharp/sshmail/internal/notify"
	"github.com/rolandnsharp/sshmail/internal/store"
)

// Event is pushed to watchers when something happens.
type Event struct {
	Type    string `json:"type"`              // "message", "read", etc.
	From    string `json:"from,omitempty"`
	To      string `json:"to,omitempty"`
	Body    string `json:"body,omitempty"`
	ID      int64  `json:"id,omitempty"`
	At      string `json:"at,omitempty"`
}

// Hub manages event subscriptions.
type Hub struct {
	mu   sync.RWMutex
	subs map[int64]map[chan Event]struct{} // agentID → set of channels
}

func NewHub() *Hub {
	return &Hub{subs: make(map[int64]map[chan Event]struct{})}
}

func (h *Hub) Subscribe(agentID int64) chan Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan Event, 16)
	if h.subs[agentID] == nil {
		h.subs[agentID] = make(map[chan Event]struct{})
	}
	h.subs[agentID][ch] = struct{}{}
	return ch
}

func (h *Hub) Unsubscribe(agentID int64, ch chan Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if m, ok := h.subs[agentID]; ok {
		delete(m, ch)
		if len(m) == 0 {
			delete(h.subs, agentID)
		}
	}
	close(ch)
}

// Notify sends an event to all watchers of the given agent IDs.
func (h *Hub) Notify(agentIDs []int64, evt Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, id := range agentIDs {
		for ch := range h.subs[id] {
			select {
			case ch <- evt:
			default: // drop if watcher is slow
			}
		}
	}
}

type Handler struct {
	Store    store.Store
	DataDir  string
	Events   *Hub
	Notifier *notify.Notifier // nil means email notifications disabled
}

func (h *Handler) Handle(sess ssh.Session) {
	cmd := sess.Command()
	if len(cmd) == 0 {
		h.handleHelp(sess)
		return
	}

	agent := auth.AgentFromContext(sess.Context())

	// invite redemption works without auth
	if cmd[0] == "invite" && len(cmd) >= 3 {
		h.handleInviteRedeem(sess, cmd)
		return
	}

	if agent == nil {
		writeJSON(sess, map[string]any{"error": "not authenticated"})
		return
	}

	switch cmd[0] {
	case "help":
		h.handleHelp(sess)
	case "whoami":
		h.handleWhoami(sess, agent)
	case "agents":
		h.handleAgents(sess)
	case "pubkey":
		h.handlePubkey(sess, cmd)
	case "bio":
		h.handleBio(sess, cmd, agent)
	case "email":
		h.handleEmail(sess, cmd, agent)
	case "send":
		h.handleSend(sess, cmd, agent)
	case "inbox":
		h.handleInbox(sess, cmd, agent)
	case "read":
		h.handleRead(sess, cmd, agent)
	case "fetch":
		h.handleFetch(sess, cmd, agent)
	case "poll":
		h.handlePoll(sess, cmd, agent)
	case "board":
		h.handleBoard(sess, cmd, agent)
	case "channel":
		h.handleChannel(sess, cmd)
	case "addkey":
		h.handleAddKey(sess, agent)
	case "keys":
		h.handleKeys(sess, agent)
	case "group":
		h.handleGroup(sess, cmd, agent)
	case "invite":
		h.handleInviteCreate(sess, agent)
	case "watch":
		h.handleWatch(sess, agent)
	case "git-upload-pack":
		h.handleGitUploadPack(sess, cmd, agent)
	case "git-receive-pack":
		h.handleGitReceivePack(sess, cmd, agent)
	default:
		writeJSON(sess, map[string]any{"error": fmt.Sprintf("unknown command: %s", cmd[0])})
	}
}

func (h *Handler) handleHelp(sess ssh.Session) {
	writeJSON(sess, map[string]any{
		"commands": []map[string]string{
			{"cmd": "send <agent> <message>", "desc": "send a text message"},
			{"cmd": "send <agent> <message> --file <name>", "desc": "send a message with file (pipe file to stdin)"},
			{"cmd": "inbox", "desc": "list unread messages"},
			{"cmd": "inbox --all", "desc": "list all messages"},
			{"cmd": "read <id>", "desc": "read a message (marks as read)"},
			{"cmd": "fetch <id>", "desc": "fetch file attachment (writes to stdout, marks as read)"},
			{"cmd": "poll", "desc": "check unread message count"},
			{"cmd": "board", "desc": "read the public board"},
			{"cmd": "board <name>", "desc": "read a public agent's messages"},
			{"cmd": "channel <name> [description]", "desc": "create a public channel"},
			{"cmd": "group create <name> [description]", "desc": "create a private group"},
			{"cmd": "group add <group> <agent>", "desc": "add a member (any member can)"},
			{"cmd": "group remove <group> <agent>", "desc": "remove a member (admin only)"},
			{"cmd": "group members <group>", "desc": "list group members"},
			{"cmd": "agents", "desc": "list all agents"},
			{"cmd": "pubkey <agent>", "desc": "get an agent's public key (for encryption)"},
			{"cmd": "whoami", "desc": "show your agent info"},
			{"cmd": "bio <text>", "desc": "set your bio"},
			{"cmd": "email", "desc": "show your email"},
			{"cmd": "email <address>", "desc": "set email for notifications"},
			{"cmd": "email clear", "desc": "remove your email"},
			{"cmd": "addkey", "desc": "add an SSH key (pipe pubkey to stdin)"},
			{"cmd": "keys", "desc": "list your SSH keys"},
			{"cmd": "invite", "desc": "generate an invite code"},
			{"cmd": "invite <code> <name>", "desc": "redeem invite (pipe pubkey to stdin)"},
			{"cmd": "help", "desc": "show this help"},
		},
	})
}

func (h *Handler) handleWhoami(sess ssh.Session, agent *store.Agent) {
	writeJSON(sess, agent)
}

func (h *Handler) handlePubkey(sess ssh.Session, cmd []string) {
	if len(cmd) < 2 {
		writeJSON(sess, map[string]any{"error": "usage: pubkey <agent>"})
		return
	}
	agent, ok := h.requireAgent(sess, cmd[1])
	if !ok {
		return
	}
	// Raw output so it can be piped directly into age -R
	fmt.Fprintln(sess, agent.PublicKey)
}

func (h *Handler) handleAgents(sess ssh.Session) {
	agents, err := h.Store.ListAgents()
	if err != nil {
		writeErr(sess, err)
		return
	}
	writeJSON(sess, map[string]any{"agents": agents})
}

func (h *Handler) handleBio(sess ssh.Session, cmd []string, agent *store.Agent) {
	if len(cmd) < 2 {
		writeJSON(sess, map[string]any{"error": "usage: bio <text>"})
		return
	}
	bio := strings.Join(cmd[1:], " ")
	if err := h.Store.UpdateBio(agent.ID, bio); err != nil {
		writeErr(sess, err)
		return
	}
	writeJSON(sess, map[string]any{"ok": true})
}

func (h *Handler) handleEmail(sess ssh.Session, cmd []string, agent *store.Agent) {
	if len(cmd) < 2 {
		// Show current email
		if agent.Email != nil {
			writeJSON(sess, map[string]any{"email": *agent.Email})
		} else {
			writeJSON(sess, map[string]any{"email": nil})
		}
		return
	}
	if cmd[1] == "clear" {
		if err := h.Store.UpdateEmail(agent.ID, nil); err != nil {
			writeErr(sess, err)
			return
		}
		writeJSON(sess, map[string]any{"ok": true, "email": nil})
		return
	}
	email := cmd[1]
	if !strings.Contains(email, "@") {
		writeJSON(sess, map[string]any{"error": "invalid email address"})
		return
	}
	// Check uniqueness
	existing, err := h.Store.AgentByEmail(email)
	if err != nil {
		writeErr(sess, err)
		return
	}
	if existing != nil && existing.ID != agent.ID {
		writeJSON(sess, map[string]any{"error": "email already in use by another agent"})
		return
	}
	if err := h.Store.UpdateEmail(agent.ID, &email); err != nil {
		writeErr(sess, err)
		return
	}
	writeJSON(sess, map[string]any{"ok": true, "email": email})
}

func (h *Handler) handleAddKey(sess ssh.Session, agent *store.Agent) {
	pubKeyData, err := io.ReadAll(io.LimitReader(sess, 8192))
	if err != nil {
		writeErr(sess, err)
		return
	}
	pubKeyStr := strings.TrimSpace(string(pubKeyData))
	if pubKeyStr == "" {
		writeJSON(sess, map[string]any{"error": "pipe your public key to stdin: cat ~/.ssh/id_ed25519.pub | ssh ssh.sshmail.dev addkey"})
		return
	}
	pubKey, _, _, _, err := gossh.ParseAuthorizedKey([]byte(pubKeyStr))
	if err != nil {
		writeJSON(sess, map[string]any{"error": fmt.Sprintf("invalid public key: %v", err)})
		return
	}
	fingerprint := gossh.FingerprintSHA256(pubKey)

	if err := h.Store.AddKey(agent.ID, fingerprint, pubKeyStr); err != nil {
		writeErr(sess, err)
		return
	}
	writeJSON(sess, map[string]any{"ok": true, "fingerprint": fingerprint})
}

func (h *Handler) handleKeys(sess ssh.Session, agent *store.Agent) {
	keys, err := h.Store.ListKeys(agent.ID)
	if err != nil {
		writeErr(sess, err)
		return
	}
	writeJSON(sess, map[string]any{"keys": keys})
}

func (h *Handler) handleSend(sess ssh.Session, cmd []string, agent *store.Agent) {
	// send <agent> <message> [--file <name>]
	if len(cmd) < 3 {
		writeJSON(sess, map[string]any{"error": "usage: send <agent> <message> [--file <name>]"})
		return
	}

	toName := cmd[1]
	to, ok := h.requireAgent(sess, toName)
	if !ok {
		return
	}

	// If sending to a private group, check membership
	if !to.Public && to.PublicKey == "" {
		isMember, err := h.Store.IsGroupMember(to.ID, agent.ID)
		if err != nil {
			writeErr(sess, err)
			return
		}
		if !isMember {
			writeJSON(sess, map[string]any{"error": "you are not a member of this group"})
			return
		}
	}

	// Parse message and --file flag
	var message string
	var fileName *string
	args := cmd[2:]
	for i := 0; i < len(args); i++ {
		if args[i] == "--file" && i+1 < len(args) {
			name := args[i+1]
			fileName = &name
			i++ // skip next
		} else {
			if message != "" {
				message += " "
			}
			message += args[i]
		}
	}

	if message == "" && fileName != nil {
		message = fmt.Sprintf("sent a file: %s", *fileName)
	}

	var filePath *string
	if fileName != nil {
		// Read file from stdin
		filesDir := filepath.Join(h.DataDir, "files")
		if err := os.MkdirAll(filesDir, 0o755); err != nil {
			writeErr(sess, err)
			return
		}
		diskName := uuid.New().String() + "-" + filepath.Base(*fileName)
		diskPath := filepath.Join(filesDir, diskName)

		f, err := os.Create(diskPath)
		if err != nil {
			writeErr(sess, err)
			return
		}
		// Limit file uploads to 50MB
		if _, err := io.Copy(f, io.LimitReader(sess, 50<<20)); err != nil {
			f.Close()
			os.Remove(diskPath)
			writeErr(sess, err)
			return
		}
		f.Close()
		filePath = &diskPath
	}

	id, err := h.Store.SendMessage(agent.ID, to.ID, message, fileName, filePath)
	if err != nil {
		writeErr(sess, err)
		return
	}

	now := time.Now()

	// Notify watchers
	evt := Event{
		Type: "message",
		From: agent.Name,
		To:   to.Name,
		Body: message,
		ID:   id,
		At:   now.UTC().Format(time.RFC3339),
	}
	if h.Events != nil {
		go func() {
			if to.Public || to.PublicKey == "" {
				// Board or group: notify all members
				members, err := h.Store.GroupMembers(to.ID)
				if err == nil {
					ids := make([]int64, len(members))
					for i, m := range members {
						ids[i] = m.AgentID
					}
					h.Events.Notify(ids, evt)
				}
			} else {
				// DM: notify recipient and sender
				h.Events.Notify([]int64{to.ID, agent.ID}, evt)
			}
		}()
	}

	// Email notifications (non-blocking, non-fatal)
	if h.Notifier != nil {
		go func() {
			if to.Public || to.PublicKey == "" {
				// Group/board: email all members except sender
				members, err := h.Store.GroupMembers(to.ID)
				if err != nil {
					fmt.Fprintf(os.Stderr, "email notify: list members: %v\n", err)
					return
				}
				for _, m := range members {
					if m.AgentID == agent.ID {
						continue
					}
					member, err := h.Store.AgentByID(m.AgentID)
					if err != nil || member == nil || member.Email == nil {
						continue
					}
					subject := fmt.Sprintf("New message in %s from %s", to.Name, agent.Name)
					body := fmt.Sprintf("%s wrote in %s:\n\n%s", agent.Name, to.Name, message)
					if err := h.Notifier.Send(*member.Email, subject, body); err != nil {
						fmt.Fprintf(os.Stderr, "email notify %s: %v\n", *member.Email, err)
					}
				}
			} else {
				// DM: email the recipient if they have an email set
				if to.Email != nil {
					subject := fmt.Sprintf("New message from %s", agent.Name)
					body := fmt.Sprintf("%s sent you a message:\n\n%s", agent.Name, message)
					if err := h.Notifier.Send(*to.Email, subject, body); err != nil {
						fmt.Fprintf(os.Stderr, "email notify %s: %v\n", *to.Email, err)
					}
				}
			}
		}()
	}

	// Commit message to recipient's git repo (non-blocking, non-fatal)
	go func() {
		var repoName, msgPath string
		if to.Public || to.PublicKey == "" {
			repoName = to.Name
			msgPath = "messages/" + agent.Name + ".md"
		} else {
			repoName = to.Name
			msgPath = "messages/direct/" + agent.Name + ".md"
		}
		if err := h.CommitMessage(repoName, msgPath, agent.Name, message, now); err != nil {
			fmt.Fprintf(os.Stderr, "git commit error: %v\n", err)
		}
	}()

	writeJSON(sess, map[string]any{"ok": true, "id": id})
}

func (h *Handler) handleInbox(sess ssh.Session, cmd []string, agent *store.Agent) {
	var all bool
	var after *time.Time
	for _, arg := range cmd[1:] {
		if arg == "--all" {
			all = true
		} else if strings.HasPrefix(arg, "--after=") {
			ts, err := time.Parse(time.RFC3339, strings.TrimPrefix(arg, "--after="))
			if err != nil {
				writeJSON(sess, map[string]any{"error": "bad --after timestamp, use RFC3339"})
				return
			}
			after = &ts
		}
	}
	msgs, err := h.Store.Inbox(agent.ID, all, after)
	if err != nil {
		writeErr(sess, err)
		return
	}
	if msgs == nil {
		msgs = []store.Message{}
	}
	writeJSON(sess, map[string]any{"messages": msgs})
}

func (h *Handler) handleRead(sess ssh.Session, cmd []string, agent *store.Agent) {
	if len(cmd) < 2 {
		writeJSON(sess, map[string]any{"error": "usage: read <id>"})
		return
	}
	var id int64
	fmt.Sscan(cmd[1], &id)
	if id == 0 {
		writeJSON(sess, map[string]any{"error": "invalid message id"})
		return
	}

	msg, err := h.Store.GetMessage(id)
	if err != nil {
		writeErr(sess, err)
		return
	}
	if msg == nil {
		writeJSON(sess, map[string]any{"error": "message not found"})
		return
	}
	if !h.canAccessMessage(msg, agent.ID) {
		writeJSON(sess, map[string]any{"error": "message not found"})
		return
	}
	if msg.ToID == agent.ID {
		h.Store.MarkRead(id)
	}
	writeJSON(sess, msg)
}

func (h *Handler) handleFetch(sess ssh.Session, cmd []string, agent *store.Agent) {
	if len(cmd) < 2 {
		writeJSON(sess, map[string]any{"error": "usage: fetch <id>"})
		return
	}
	var id int64
	fmt.Sscan(cmd[1], &id)
	if id == 0 {
		writeJSON(sess, map[string]any{"error": "invalid message id"})
		return
	}

	msg, err := h.Store.GetMessage(id)
	if err != nil {
		writeErr(sess, err)
		return
	}
	if msg == nil {
		writeJSON(sess, map[string]any{"error": "message not found"})
		return
	}
	if !h.canAccessMessage(msg, agent.ID) {
		writeJSON(sess, map[string]any{"error": "message not found"})
		return
	}

	if msg.FilePath == nil {
		// No file — just return the message as JSON
		if msg.ToID == agent.ID {
			h.Store.MarkRead(id)
		}
		writeJSON(sess, msg)
		return
	}

	// Stream file to stdout
	f, err := os.Open(*msg.FilePath)
	if err != nil {
		writeErr(sess, err)
		return
	}
	defer f.Close()
	io.Copy(sess, f)

	if msg.ToID == agent.ID {
		h.Store.MarkRead(id)
	}
}

func (h *Handler) handleChannel(sess ssh.Session, cmd []string) {
	if len(cmd) < 2 {
		writeJSON(sess, map[string]any{"error": "usage: channel <name> [description]"})
		return
	}
	name := cmd[1]
	bio := ""
	if len(cmd) >= 3 {
		bio = strings.Join(cmd[2:], " ")
	}
	ch, err := h.Store.CreateChannel(name, bio)
	if err != nil {
		writeErr(sess, err)
		return
	}
	writeJSON(sess, map[string]any{"ok": true, "channel": ch.Name})
}

func (h *Handler) handleGroup(sess ssh.Session, cmd []string, agent *store.Agent) {
	if len(cmd) < 2 {
		writeJSON(sess, map[string]any{"error": "usage: group <create|add|remove|members> ..."})
		return
	}
	switch cmd[1] {
	case "create":
		if len(cmd) < 3 {
			writeJSON(sess, map[string]any{"error": "usage: group create <name> [description]"})
			return
		}
		name := cmd[2]
		bio := ""
		if len(cmd) >= 4 {
			bio = strings.Join(cmd[3:], " ")
		}
		grp, err := h.Store.CreateGroup(name, bio, agent.ID)
		if err != nil {
			writeErr(sess, err)
			return
		}
		writeJSON(sess, map[string]any{"ok": true, "group": grp.Name})
	case "add":
		if len(cmd) < 4 {
			writeJSON(sess, map[string]any{"error": "usage: group add <group> <agent>"})
			return
		}
		grp, ok := h.requireAgent(sess, cmd[2])
		if !ok {
			return
		}
		isMember, err := h.Store.IsGroupMember(grp.ID, agent.ID)
		if err != nil {
			writeErr(sess, err)
			return
		}
		if !isMember {
			writeJSON(sess, map[string]any{"error": "you are not a member of this group"})
			return
		}
		target, ok := h.requireAgent(sess, cmd[3])
		if !ok {
			return
		}
		if err := h.Store.AddGroupMember(grp.ID, target.ID); err != nil {
			writeErr(sess, err)
			return
		}
		writeJSON(sess, map[string]any{"ok": true})
	case "remove":
		if len(cmd) < 4 {
			writeJSON(sess, map[string]any{"error": "usage: group remove <group> <agent>"})
			return
		}
		grp, ok := h.requireAgent(sess, cmd[2])
		if !ok {
			return
		}
		target, ok := h.requireAgent(sess, cmd[3])
		if !ok {
			return
		}
		// Only admin can remove others, members can remove themselves
		role, err := h.Store.GroupRole(grp.ID, agent.ID)
		if err != nil {
			writeErr(sess, err)
			return
		}
		if target.ID != agent.ID && role != "admin" {
			writeJSON(sess, map[string]any{"error": "only the group admin can remove others"})
			return
		}
		// Prevent admin from orphaning the group
		if target.ID == agent.ID && role == "admin" {
			writeJSON(sess, map[string]any{"error": "admin cannot leave the group — transfer admin first"})
			return
		}
		if err := h.Store.RemoveGroupMember(grp.ID, target.ID); err != nil {
			writeErr(sess, err)
			return
		}
		writeJSON(sess, map[string]any{"ok": true})
	case "members":
		if len(cmd) < 3 {
			writeJSON(sess, map[string]any{"error": "usage: group members <group>"})
			return
		}
		grp, ok := h.requireAgent(sess, cmd[2])
		if !ok {
			return
		}
		isMember, err := h.Store.IsGroupMember(grp.ID, agent.ID)
		if err != nil {
			writeErr(sess, err)
			return
		}
		if !isMember {
			writeJSON(sess, map[string]any{"error": "you are not a member of this group"})
			return
		}
		members, err := h.Store.GroupMembers(grp.ID)
		if err != nil {
			writeErr(sess, err)
			return
		}
		writeJSON(sess, map[string]any{"group": grp.Name, "members": members})
	default:
		writeJSON(sess, map[string]any{"error": "usage: group <create|add|remove|members> ..."})
	}
}

func (h *Handler) handleBoard(sess ssh.Session, cmd []string, agent *store.Agent) {
	boardName := "board"
	if len(cmd) >= 2 {
		boardName = cmd[1]
	}
	target, ok := h.requireAgent(sess, boardName)
	if !ok {
		return
	}
	if !target.Public {
		writeJSON(sess, map[string]any{"error": fmt.Sprintf("%s is not a public board", boardName)})
		return
	}
	msgs, err := h.Store.Inbox(target.ID, true, nil)
	if err != nil {
		writeErr(sess, err)
		return
	}
	if msgs == nil {
		msgs = []store.Message{}
	}
	writeJSON(sess, map[string]any{"board": boardName, "messages": msgs})
}

func (h *Handler) handlePoll(sess ssh.Session, cmd []string, agent *store.Agent) {
	if len(cmd) > 1 && cmd[1] == "--counts" {
		counts, err := h.Store.UnreadCounts(agent.ID)
		if err != nil {
			writeErr(sess, err)
			return
		}
		total := 0
		for _, c := range counts {
			total += c
		}
		writeJSON(sess, map[string]any{"unread": total, "counts": counts})
		return
	}
	count, err := h.Store.UnreadCount(agent.ID)
	if err != nil {
		writeErr(sess, err)
		return
	}
	writeJSON(sess, map[string]any{"unread": count})
}

func (h *Handler) handleWatch(sess ssh.Session, agent *store.Agent) {
	if h.Events == nil {
		writeJSON(sess, map[string]any{"error": "event hub not initialized"})
		return
	}

	ch := h.Events.Subscribe(agent.ID)
	defer h.Events.Unsubscribe(agent.ID, ch)

	enc := json.NewEncoder(sess)
	enc.SetEscapeHTML(false)

	// Send a connected event so the client knows the stream is alive
	enc.Encode(Event{Type: "connected"})

	ctx := sess.Context()
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				return
			}
			if err := enc.Encode(evt); err != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func (h *Handler) handleInviteCreate(sess ssh.Session, agent *store.Agent) {
	code, err := h.Store.CreateInvite(agent.ID)
	if err != nil {
		writeErr(sess, err)
		return
	}
	writeJSON(sess, map[string]any{
		"code":   code,
		"redeem": fmt.Sprintf("ssh ssh.sshmail.dev invite %s <agent-name> < ~/.ssh/id_ed25519.pub", code),
	})
}

func (h *Handler) handleInviteRedeem(sess ssh.Session, cmd []string) {
	code := cmd[1]
	name := cmd[2]

	// Validate agent name: alphanumeric, hyphens, underscores, 1-32 chars
	if len(name) == 0 || len(name) > 32 {
		writeJSON(sess, map[string]any{"error": "name must be 1-32 characters"})
		return
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			writeJSON(sess, map[string]any{"error": "name must be lowercase alphanumeric, hyphens, or underscores"})
			return
		}
	}

	pubKeyData, err := io.ReadAll(io.LimitReader(sess, 8192))
	if err != nil {
		writeErr(sess, err)
		return
	}
	pubKeyStr := strings.TrimSpace(string(pubKeyData))
	if pubKeyStr == "" {
		writeJSON(sess, map[string]any{"error": "no public key on stdin"})
		return
	}
	pubKey, _, _, _, err := gossh.ParseAuthorizedKey([]byte(pubKeyStr))
	if err != nil {
		writeJSON(sess, map[string]any{"error": fmt.Sprintf("invalid public key: %v", err)})
		return
	}
	fingerprint := gossh.FingerprintSHA256(pubKey)

	agent, err := h.Store.RedeemInvite(code, name, fingerprint, pubKeyStr)
	if err != nil {
		writeJSON(sess, map[string]any{"error": err.Error()})
		return
	}

	// Init git repo for new agent
	if err := h.InitRepo(agent.Name); err != nil {
		// Non-fatal: agent is created, repo can be initialized later
		fmt.Fprintf(sess.Stderr(), "warning: failed to init repo: %v\n", err)
	}

	writeJSON(sess, map[string]any{"ok": true, "name": agent.Name, "id": agent.ID})
}

// --- Git ---

// RepoPath returns the absolute path to an agent's bare git repo.
func (h *Handler) RepoPath(name string) string {
	p := filepath.Join(h.DataDir, "repos", name+".git")
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

// InitRepo creates a bare git repo for an agent if it doesn't exist.
func (h *Handler) InitRepo(name string) error {
	rp := h.RepoPath(name)
	if _, err := os.Stat(rp); err == nil {
		return nil // already exists
	}
	cmd := exec.Command("git", "init", "--bare", rp)
	return cmd.Run()
}

// CommitMessage appends a message to a markdown file in a bare git repo.
// For DMs, path is "messages/direct/<from>.md". For groups/boards, path is "messages/<from>.md".
func (h *Handler) CommitMessage(repoName, filePath, from, body string, at time.Time) error {
	fmt.Fprintf(os.Stderr, "CommitMessage: repo=%s file=%s from=%s\n", repoName, filePath, from)
	if err := h.InitRepo(repoName); err != nil {
		return fmt.Errorf("init repo: %w", err)
	}

	rp := h.RepoPath(repoName)
	fmt.Fprintf(os.Stderr, "CommitMessage: repoPath=%s\n", rp)

	// Use a temp worktree to commit into the bare repo
	tmpDir, err := os.MkdirTemp("", "sshmail-commit-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	// Init a temp working dir and point it at the bare repo
	initCmd := exec.Command("git", "init", tmpDir)
	initCmd.Stdout, initCmd.Stderr = io.Discard, io.Discard
	if err := initCmd.Run(); err != nil {
		return fmt.Errorf("init temp: %w", err)
	}

	remoteCmd := exec.Command("git", "-C", tmpDir, "remote", "add", "origin", rp)
	remoteCmd.Run()

	// Pull existing content if the repo has commits
	pullCmd := exec.Command("git", "-C", tmpDir, "pull", "origin", "master")
	pullCmd.Stdout, pullCmd.Stderr = io.Discard, io.Discard
	pullCmd.Run() // OK to fail on empty repo

	// Ensure directory exists
	fullPath := filepath.Join(tmpDir, filePath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return err
	}

	// Format the message entry
	timestamp := at.UTC().Format(time.RFC3339)
	entry := fmt.Sprintf("\n---\n**%s** _%s_\n\n%s\n", from, timestamp, body)

	// Append to file (prepend to keep newest-first)
	existing, _ := os.ReadFile(fullPath)
	if err := os.WriteFile(fullPath, []byte(entry+string(existing)), 0o644); err != nil {
		return err
	}

	// Stage, commit, push
	addCmd := exec.Command("git", "-C", tmpDir, "add", filePath)
	if err := addCmd.Run(); err != nil {
		return fmt.Errorf("git add: %w", err)
	}

	commitMsg := fmt.Sprintf("message from %s", from)
	commitCmd := exec.Command("git", "-C", tmpDir,
		"-c", "user.name=sshmail",
		"-c", "user.email=hub@sshmail.dev",
		"commit", "-m", commitMsg)
	commitCmd.Stdout, commitCmd.Stderr = io.Discard, io.Discard
	if err := commitCmd.Run(); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}

	pushCmd := exec.Command("git", "-C", tmpDir, "push", "origin", "HEAD")
	pushCmd.Stdout, pushCmd.Stderr = io.Discard, io.Discard
	if err := pushCmd.Run(); err != nil {
		return fmt.Errorf("git push: %w", err)
	}

	return nil
}

func (h *Handler) handleGitUploadPack(sess ssh.Session, cmd []string, agent *store.Agent) {
	if len(cmd) < 2 {
		fmt.Fprintln(sess.Stderr(), "usage: git-upload-pack <repo>")
		return
	}

	repoName := sanitizeRepoName(cmd[1])
	rp := h.RepoPath(repoName)

	if _, err := os.Stat(rp); os.IsNotExist(err) {
		fmt.Fprintf(sess.Stderr(), "repository not found: %s\n", repoName)
		return
	}

	// Anyone can pull their own repo. Public agents' repos are readable by all.
	if repoName != agent.Name {
		target, err := h.Store.AgentByName(repoName)
		if err != nil || target == nil || !target.Public {
			fmt.Fprintf(sess.Stderr(), "access denied: %s\n", repoName)
			return
		}
	}

	c := exec.CommandContext(sess.Context(), "git", "upload-pack", rp)
	c.Stdin = sess
	c.Stdout = sess
	c.Stderr = sess.Stderr()
	c.Run()
}

func (h *Handler) handleGitReceivePack(sess ssh.Session, cmd []string, agent *store.Agent) {
	if len(cmd) < 2 {
		fmt.Fprintln(sess.Stderr(), "usage: git-receive-pack <repo>")
		return
	}

	repoName := sanitizeRepoName(cmd[1])

	// Agents can only push to their own repo
	if repoName != agent.Name {
		fmt.Fprintf(sess.Stderr(), "access denied: can only push to your own repo\n")
		return
	}

	rp := h.RepoPath(repoName)

	// Auto-init repo on first push
	if err := h.InitRepo(repoName); err != nil {
		fmt.Fprintf(sess.Stderr(), "failed to init repo: %v\n", err)
		return
	}

	c := exec.CommandContext(sess.Context(), "git", "receive-pack", rp)
	c.Stdin = sess
	c.Stdout = sess
	c.Stderr = sess.Stderr()
	c.Run()
}

func sanitizeRepoName(name string) string {
	name = strings.TrimPrefix(name, "/")
	name = strings.TrimSuffix(name, ".git")
	name = filepath.Base(name) // prevent path traversal
	return name
}

// canAccessMessage checks if an agent can access a message (sender, recipient, or group member).
func (h *Handler) canAccessMessage(msg *store.Message, agentID int64) bool {
	if msg.ToID == agentID || msg.FromID == agentID {
		return true
	}
	isMember, _ := h.Store.IsGroupMember(msg.ToID, agentID)
	return isMember
}

// requireAgent looks up an agent by name and writes an error to the session if not found.
// Returns the agent and true on success, or nil and false on failure.
func (h *Handler) requireAgent(sess ssh.Session, name string) (*store.Agent, bool) {
	agent, err := h.Store.AgentByName(name)
	if err != nil {
		writeErr(sess, err)
		return nil, false
	}
	if agent == nil {
		writeJSON(sess, map[string]any{"error": fmt.Sprintf("agent not found: %s", name)})
		return nil, false
	}
	return agent, true
}

func writeJSON(w io.Writer, v any) {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

func writeErr(w io.Writer, err error) {
	writeJSON(w, map[string]any{"error": err.Error()})
}
