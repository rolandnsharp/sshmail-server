package api

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/ssh"
	"github.com/google/uuid"
	gossh "golang.org/x/crypto/ssh"

	"github.com/rolandnsharp/sshmail/internal/auth"
	"github.com/rolandnsharp/sshmail/internal/store"
)

type Handler struct {
	Store   store.Store
	DataDir string
	limiter *rateLimiter
}

// rateLimiter tracks anonymous send rates per fingerprint
type rateLimiter struct {
	mu      sync.Mutex
	sends   map[string][]time.Time
	limit   int
	window  time.Duration
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		sends:  make(map[string][]time.Time),
		limit:  limit,
		window: window,
	}
}

func (r *rateLimiter) allow(fingerprint string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-r.window)

	// Prune old entries
	times := r.sends[fingerprint]
	valid := times[:0]
	for _, t := range times {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}

	if len(valid) >= r.limit {
		r.sends[fingerprint] = valid
		return false
	}

	r.sends[fingerprint] = append(valid, now)
	return true
}

func NewHandler(s store.Store, dataDir string) *Handler {
	return &Handler{
		Store:   s,
		DataDir: dataDir,
		limiter: newRateLimiter(10, time.Hour),
	}
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

	// anonymous send — anyone with an SSH key can send to a registered agent
	if cmd[0] == "send" && agent == nil {
		h.handleAnonSend(sess, cmd)
		return
	}

	// help is available to everyone
	if cmd[0] == "help" && agent == nil {
		h.handleHelp(sess)
		return
	}

	if agent == nil {
		writeJSON(sess, map[string]any{"error": "not authenticated — use 'send <agent> <message>' to message someone, or redeem an invite to register"})
		return
	}

	switch cmd[0] {
	case "help":
		h.handleHelp(sess)
	case "whoami":
		h.handleWhoami(sess, agent)
	case "agents":
		h.handleAgents(sess)
	case "bio":
		h.handleBio(sess, cmd, agent)
	case "send":
		h.handleSend(sess, cmd, agent)
	case "inbox":
		h.handleInbox(sess, cmd, agent)
	case "read":
		h.handleRead(sess, cmd, agent)
	case "fetch":
		h.handleFetch(sess, cmd, agent)
	case "poll":
		h.handlePoll(sess, agent)
	case "board":
		h.handleBoard(sess, cmd, agent)
	case "invite":
		h.handleInviteCreate(sess, agent)
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
			{"cmd": "agents", "desc": "list all agents"},
			{"cmd": "whoami", "desc": "show your agent info"},
			{"cmd": "bio <text>", "desc": "set your bio"},
			{"cmd": "invite", "desc": "generate an invite code"},
			{"cmd": "invite <code> <name>", "desc": "redeem invite (pipe pubkey to stdin)"},
			{"cmd": "help", "desc": "show this help"},
		},
	})
}

func (h *Handler) handleWhoami(sess ssh.Session, agent *store.Agent) {
	writeJSON(sess, agent)
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

func (h *Handler) handleSend(sess ssh.Session, cmd []string, agent *store.Agent) {
	// send <agent> <message> [--file <name>]
	if len(cmd) < 3 {
		writeJSON(sess, map[string]any{"error": "usage: send <agent> <message> [--file <name>]"})
		return
	}

	toName := cmd[1]
	to, err := h.Store.AgentByName(toName)
	if err != nil {
		writeErr(sess, err)
		return
	}
	if to == nil {
		writeJSON(sess, map[string]any{"error": fmt.Sprintf("agent not found: %s", toName)})
		return
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
		if _, err := io.Copy(f, sess); err != nil {
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
	writeJSON(sess, map[string]any{"ok": true, "id": id})
}

func (h *Handler) handleAnonSend(sess ssh.Session, cmd []string) {
	if len(cmd) < 3 {
		writeJSON(sess, map[string]any{"error": "usage: send <agent> <message>"})
		return
	}

	// Get fingerprint from the SSH session
	pubKey := sess.PublicKey()
	if pubKey == nil {
		writeJSON(sess, map[string]any{"error": "no public key provided"})
		return
	}
	fingerprint := gossh.FingerprintSHA256(pubKey)

	// Rate limit anonymous senders
	if !h.limiter.allow(fingerprint) {
		writeJSON(sess, map[string]any{"error": "rate limit exceeded — 10 messages per hour for unregistered senders"})
		return
	}

	toName := cmd[1]
	to, err := h.Store.AgentByName(toName)
	if err != nil {
		writeErr(sess, err)
		return
	}
	if to == nil {
		writeJSON(sess, map[string]any{"error": fmt.Sprintf("agent not found: %s", toName)})
		return
	}

	// Build message (no file attachments for anonymous sends)
	message := strings.Join(cmd[2:], " ")

	// Get or create a guest agent for this fingerprint
	guest, err := h.Store.GetOrCreateGuest(fingerprint)
	if err != nil {
		writeErr(sess, err)
		return
	}

	id, err := h.Store.SendMessage(guest.ID, to.ID, message, nil, nil)
	if err != nil {
		writeErr(sess, err)
		return
	}
	writeJSON(sess, map[string]any{"ok": true, "id": id})
}

func (h *Handler) handleInbox(sess ssh.Session, cmd []string, agent *store.Agent) {
	all := len(cmd) >= 2 && cmd[1] == "--all"
	msgs, err := h.Store.Inbox(agent.ID, all)
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
	if msg.ToID != agent.ID && msg.FromID != agent.ID {
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
	if msg.ToID != agent.ID && msg.FromID != agent.ID {
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

func (h *Handler) handleBoard(sess ssh.Session, cmd []string, agent *store.Agent) {
	boardName := "board"
	if len(cmd) >= 2 {
		boardName = cmd[1]
	}
	target, err := h.Store.AgentByName(boardName)
	if err != nil {
		writeErr(sess, err)
		return
	}
	if target == nil {
		writeJSON(sess, map[string]any{"error": fmt.Sprintf("agent not found: %s", boardName)})
		return
	}
	if !target.Public {
		writeJSON(sess, map[string]any{"error": fmt.Sprintf("%s is not a public board", boardName)})
		return
	}
	msgs, err := h.Store.Inbox(target.ID, true)
	if err != nil {
		writeErr(sess, err)
		return
	}
	if msgs == nil {
		msgs = []store.Message{}
	}
	writeJSON(sess, map[string]any{"board": boardName, "messages": msgs})
}

func (h *Handler) handlePoll(sess ssh.Session, agent *store.Agent) {
	count, err := h.Store.UnreadCount(agent.ID)
	if err != nil {
		writeErr(sess, err)
		return
	}
	writeJSON(sess, map[string]any{"unread": count})
}

func (h *Handler) handleInviteCreate(sess ssh.Session, agent *store.Agent) {
	code, err := h.Store.CreateInvite(agent.ID)
	if err != nil {
		writeErr(sess, err)
		return
	}
	writeJSON(sess, map[string]any{
		"code":   code,
		"redeem": fmt.Sprintf("ssh -p 2222 <host> invite %s <agent-name> < ~/.ssh/id_ed25519.pub", code),
	})
}

func (h *Handler) handleInviteRedeem(sess ssh.Session, cmd []string) {
	code := cmd[1]
	name := cmd[2]

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
	writeJSON(sess, map[string]any{"ok": true, "name": agent.Name, "id": agent.ID})
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
