package tui

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rolandnsharp/sshmail/internal/api"
	"github.com/rolandnsharp/sshmail/internal/store"
)

// LocalBackend implements Backend using the store directly (server-side).
type LocalBackend struct {
	Store   store.Store
	Agent   *store.Agent
	Events  *api.Hub
	DataDir string
}

func (b *LocalBackend) Whoami() (*Agent, error) {
	a := b.Agent
	return &Agent{
		ID:          a.ID,
		Name:        a.Name,
		Fingerprint: a.Fingerprint,
		Bio:         a.Bio,
		Public:      a.Public,
		JoinedAt:    a.JoinedAt,
		InvitedBy:   a.InvitedBy,
	}, nil
}

func (b *LocalBackend) Agents() ([]Agent, error) {
	agents, err := b.Store.ListAgents()
	if err != nil {
		return nil, err
	}
	result := make([]Agent, len(agents))
	for i, a := range agents {
		result[i] = Agent{
			ID:          a.ID,
			Name:        a.Name,
			Fingerprint: a.Fingerprint,
			Bio:         a.Bio,
			Public:      a.Public,
			JoinedAt:    a.JoinedAt,
			InvitedBy:   a.InvitedBy,
		}
	}
	return result, nil
}

func (b *LocalBackend) Inbox(all bool) ([]Message, error) {
	msgs, err := b.Store.Inbox(b.Agent.ID, all, nil)
	if err != nil {
		return nil, err
	}
	result := make([]Message, len(msgs))
	for i, m := range msgs {
		result[i] = Message{
			ID:     m.ID,
			From:   m.FromName,
			To:     m.ToName,
			Body:   m.Body,
			File:   m.FileName,
			At:     m.CreatedAt,
			ReadAt: m.ReadAt,
		}
	}
	return result, nil
}

func (b *LocalBackend) Board(name string) ([]Message, error) {
	agent, err := b.Store.AgentByName(name)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return nil, fmt.Errorf("board not found: %s", name)
	}
	msgs, err := b.Store.Inbox(agent.ID, true, nil)
	if err != nil {
		return nil, err
	}
	result := make([]Message, len(msgs))
	for i, m := range msgs {
		result[i] = Message{
			ID:   m.ID,
			From: m.FromName,
			To:   m.ToName,
			Body: m.Body,
			File: m.FileName,
			At:   m.CreatedAt,
		}
	}
	return result, nil
}

func (b *LocalBackend) Send(to, message string) (*SendResult, error) {
	toAgent, err := b.Store.AgentByName(to)
	if err != nil {
		return nil, err
	}
	if toAgent == nil {
		return nil, fmt.Errorf("agent not found: %s", to)
	}

	// Check group membership for private groups
	if !toAgent.Public && toAgent.PublicKey == "" {
		isMember, err := b.Store.IsGroupMember(toAgent.ID, b.Agent.ID)
		if err != nil {
			return nil, err
		}
		if !isMember {
			return nil, fmt.Errorf("you are not a member of this group")
		}
	}

	// Strip surrounding quotes if present
	message = strings.TrimPrefix(message, "\"")
	message = strings.TrimSuffix(message, "\"")

	id, err := b.Store.SendMessage(b.Agent.ID, toAgent.ID, message, nil, nil)
	if err != nil {
		return nil, err
	}

	// Notify watchers
	if b.Events != nil {
		evt := api.Event{
			Type: "message",
			From: b.Agent.Name,
			To:   toAgent.Name,
			Body: message,
			ID:   id,
		}
		go func() {
			if toAgent.Public || toAgent.PublicKey == "" {
				members, err := b.Store.GroupMembers(toAgent.ID)
				if err == nil {
					ids := make([]int64, len(members))
					for i, m := range members {
						ids[i] = m.AgentID
					}
					b.Events.Notify(ids, evt)
				}
			} else {
				b.Events.Notify([]int64{toAgent.ID, b.Agent.ID}, evt)
			}
		}()
	}

	return &SendResult{OK: true, ID: id}, nil
}

func (b *LocalBackend) PollCounts() (*PollResult, error) {
	unread, err := b.Store.UnreadCount(b.Agent.ID)
	if err != nil {
		return nil, err
	}
	counts, err := b.Store.UnreadCounts(b.Agent.ID)
	if err != nil {
		return nil, err
	}
	return &PollResult{Unread: unread, Counts: counts}, nil
}

func (b *LocalBackend) Watch(events chan<- WatchEvent) error {
	if b.Events == nil {
		return fmt.Errorf("events not available")
	}
	ch := b.Events.Subscribe(b.Agent.ID)
	go func() {
		for evt := range ch {
			events <- WatchEvent{
				Type: evt.Type,
				From: evt.From,
				To:   evt.To,
				Body: evt.Body,
				ID:   evt.ID,
				At:   evt.At,
			}
		}
		close(events)
	}()
	return nil
}

func (b *LocalBackend) ReadFile(name string) (string, error) {
	repoPath := filepath.Join(b.DataDir, "repos", b.Agent.Name+".git")
	out, err := exec.Command("git", "--git-dir", repoPath, "show", "HEAD:"+name).Output()
	if err != nil {
		return "", fmt.Errorf("could not read %s: %w", name, err)
	}
	return string(out), nil
}

func (b *LocalBackend) Online() (map[string]bool, error) {
	if b.Events == nil {
		return nil, nil
	}
	ids := b.Events.OnlineAgentIDs()
	online := make(map[string]bool, len(ids))
	for _, id := range ids {
		agent, err := b.Store.AgentByID(id)
		if err == nil && agent != nil {
			online[agent.Name] = true
		}
	}
	return online, nil
}

func (b *LocalBackend) RepoFiles() ([]string, error) {
	repoPath := filepath.Join(b.DataDir, "repos", b.Agent.Name+".git")
	out, err := exec.Command("git", "--git-dir", repoPath, "ls-tree", "-r", "--name-only", "HEAD").Output()
	if err != nil {
		return nil, nil // empty repo or no commits
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil, nil
	}
	return lines, nil
}
