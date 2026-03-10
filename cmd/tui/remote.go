package main

import (
	"github.com/rolandnsharp/sshmail-server/internal/tui"
)

// RemoteBackend implements tui.Backend over SSH.
type RemoteBackend struct {
	client *Client
}

func (r *RemoteBackend) Whoami() (*tui.Agent, error) {
	a, err := r.client.Whoami()
	if err != nil {
		return nil, err
	}
	return &tui.Agent{
		ID:          a.ID,
		Name:        a.Name,
		Fingerprint: a.Fingerprint,
		Bio:         a.Bio,
		Public:      a.Public,
		JoinedAt:    a.JoinedAt,
		InvitedBy:   a.InvitedBy,
	}, nil
}

func (r *RemoteBackend) Agents() ([]tui.Agent, error) {
	agents, err := r.client.Agents()
	if err != nil {
		return nil, err
	}
	result := make([]tui.Agent, len(agents))
	for i, a := range agents {
		result[i] = tui.Agent{
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

func (r *RemoteBackend) Inbox(all bool) ([]tui.Message, error) {
	msgs, err := r.client.Inbox(all)
	if err != nil {
		return nil, err
	}
	result := make([]tui.Message, len(msgs))
	for i, m := range msgs {
		result[i] = tui.Message{
			ID:     m.ID,
			From:   m.From,
			To:     m.To,
			Body:   m.Body,
			File:   m.File,
			At:     m.At,
			ReadAt: m.ReadAt,
		}
	}
	return result, nil
}

func (r *RemoteBackend) Board(name string) ([]tui.Message, error) {
	msgs, err := r.client.Board(name)
	if err != nil {
		return nil, err
	}
	result := make([]tui.Message, len(msgs))
	for i, m := range msgs {
		result[i] = tui.Message{
			ID:   m.ID,
			From: m.From,
			To:   m.To,
			Body: m.Body,
			File: m.File,
			At:   m.At,
		}
	}
	return result, nil
}

func (r *RemoteBackend) Send(to, message string) (*tui.SendResult, error) {
	res, err := r.client.Send(to, message)
	if err != nil {
		return nil, err
	}
	return &tui.SendResult{OK: res.OK, ID: res.ID}, nil
}

func (r *RemoteBackend) PollCounts() (*tui.PollResult, error) {
	res, err := r.client.PollCounts()
	if err != nil {
		return nil, err
	}
	return &tui.PollResult{Unread: res.Unread, Counts: res.Counts}, nil
}

func (r *RemoteBackend) RepoFiles() ([]string, error) {
	return nil, nil // not available over remote client
}

func (r *RemoteBackend) Watch(events chan<- tui.WatchEvent) error {
	// Bridge the client's WatchEvent to tui.WatchEvent
	clientCh := make(chan WatchEvent, 16)
	if err := r.client.Watch(clientCh); err != nil {
		return err
	}
	go func() {
		for evt := range clientCh {
			events <- tui.WatchEvent{
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
