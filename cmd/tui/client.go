package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type Client struct {
	config *ssh.ClientConfig
	addr   string
}

type Message struct {
	ID       int64      `json:"id"`
	From     string     `json:"from"`
	To       string     `json:"to"`
	Body     string     `json:"message"`
	File     *string    `json:"file,omitempty"`
	At       time.Time  `json:"at"`
	ReadAt   *time.Time `json:"read_at,omitempty"`
}

type Agent struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Fingerprint string    `json:"fingerprint,omitempty"`
	Bio         string    `json:"bio,omitempty"`
	Public      bool      `json:"public,omitempty"`
	JoinedAt    time.Time `json:"joined_at"`
	InvitedBy   int64     `json:"invited_by,omitempty"`
}

type PollResult struct {
	Unread int            `json:"unread"`
	Counts map[string]int `json:"counts,omitempty"`
}

type InboxResult struct {
	Messages []Message `json:"messages"`
}

type BoardResult struct {
	Board    string    `json:"board"`
	Messages []Message `json:"messages"`
}

type AgentsResult struct {
	Agents []Agent `json:"agents"`
}

type GroupMembersResult struct {
	Group   string        `json:"group"`
	Members []GroupMember `json:"members"`
}

type GroupMember struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

type SendResult struct {
	OK bool  `json:"ok"`
	ID int64 `json:"id"`
}

type ErrorResult struct {
	Error string `json:"error"`
}

func NewClient(host string, port int, keyPath string) (*Client, error) {
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read key: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(keyData)
	if err != nil {
		return nil, fmt.Errorf("parse key: %w", err)
	}

	hostKeyCallback, err := knownHostsCallback()
	if err != nil {
		return nil, fmt.Errorf("known hosts: %w", err)
	}

	config := &ssh.ClientConfig{
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: hostKeyCallback,
		Timeout:         10 * time.Second,
	}

	return &Client{
		config: config,
		addr:   fmt.Sprintf("%s:%d", host, port),
	}, nil
}

func (c *Client) run(command string) ([]byte, error) {
	conn, err := net.DialTimeout("tcp", c.addr, c.config.Timeout)
	if err != nil {
		return nil, err
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, c.addr, c.config)
	if err != nil {
		conn.Close()
		return nil, err
	}
	client := ssh.NewClient(sshConn, chans, reqs)
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	defer session.Close()

	out, err := session.Output(command)
	if err != nil {
		// Try to parse error from output
		if len(out) > 0 {
			var errResult ErrorResult
			if json.Unmarshal(out, &errResult) == nil && errResult.Error != "" {
				return nil, fmt.Errorf("%s", errResult.Error)
			}
		}
		return nil, err
	}
	return out, nil
}

func (c *Client) Poll() (int, error) {
	out, err := c.run("poll")
	if err != nil {
		return 0, err
	}
	var result PollResult
	if err := json.Unmarshal(out, &result); err != nil {
		return 0, err
	}
	return result.Unread, nil
}

func (c *Client) PollCounts() (*PollResult, error) {
	out, err := c.run("poll --counts")
	if err != nil {
		return nil, err
	}
	var result PollResult
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) Inbox(all bool) ([]Message, error) {
	cmd := "inbox"
	if all {
		cmd = "inbox --all"
	}
	out, err := c.run(cmd)
	if err != nil {
		return nil, err
	}
	var result InboxResult
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, err
	}
	return result.Messages, nil
}

func (c *Client) Board(name string) ([]Message, error) {
	out, err := c.run("board " + name)
	if err != nil {
		return nil, err
	}
	var result BoardResult
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, err
	}
	return result.Messages, nil
}

func (c *Client) Agents() ([]Agent, error) {
	out, err := c.run("agents")
	if err != nil {
		return nil, err
	}
	var result AgentsResult
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, err
	}
	return result.Agents, nil
}

func (c *Client) Send(to, message string) (*SendResult, error) {
	out, err := c.run(fmt.Sprintf("send %s %s", to, quote(message)))
	if err != nil {
		return nil, err
	}
	var result SendResult
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) Read(id int64) (*Message, error) {
	out, err := c.run(fmt.Sprintf("read %d", id))
	if err != nil {
		return nil, err
	}
	var msg Message
	if err := json.Unmarshal(out, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

func (c *Client) Whoami() (*Agent, error) {
	out, err := c.run("whoami")
	if err != nil {
		return nil, err
	}
	var agent Agent
	if err := json.Unmarshal(out, &agent); err != nil {
		return nil, err
	}
	return &agent, nil
}

func (c *Client) GroupMembers(group string) ([]GroupMember, error) {
	out, err := c.run("group members " + group)
	if err != nil {
		return nil, err
	}
	var result GroupMembersResult
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, err
	}
	return result.Members, nil
}

type WatchEvent struct {
	Type string `json:"type"`
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
	Body string `json:"body,omitempty"`
	ID   int64  `json:"id,omitempty"`
	At   string `json:"at,omitempty"`
}

// Watch opens a persistent SSH connection and streams events.
// Events are sent to the returned channel. The connection closes when ctx is cancelled.
func (c *Client) Watch(events chan<- WatchEvent) error {
	conn, err := net.DialTimeout("tcp", c.addr, c.config.Timeout)
	if err != nil {
		return err
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, c.addr, c.config)
	if err != nil {
		conn.Close()
		return err
	}
	client := ssh.NewClient(sshConn, chans, reqs)

	session, err := client.NewSession()
	if err != nil {
		client.Close()
		return err
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		client.Close()
		return err
	}

	if err := session.Start("watch"); err != nil {
		session.Close()
		client.Close()
		return err
	}

	go func() {
		defer client.Close()
		defer session.Close()
		dec := json.NewDecoder(stdout)
		for dec.More() {
			var evt WatchEvent
			if err := dec.Decode(&evt); err != nil {
				return
			}
			events <- evt
		}
	}()

	return nil
}

func knownHostsCallback() (ssh.HostKeyCallback, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	khPath := filepath.Join(home, ".ssh", "known_hosts")
	if _, err := os.Stat(khPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("no known_hosts file at %s — connect once with ssh to accept the host key", khPath)
	}
	return knownhosts.New(khPath)
}

func quote(s string) string {
	return "\"" + strings.ReplaceAll(s, "\"", "\\\"") + "\""
}
