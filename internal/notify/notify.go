package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

// Notifier sends email notifications via the Resend HTTP API.
type Notifier struct {
	apiKey string
	from   string
}

// New creates a Notifier. Returns nil if apiKey is empty.
// If fromAddr is empty, defaults to Resend's onboarding address.
func New(apiKey, fromAddr string) *Notifier {
	if apiKey == "" {
		return nil
	}
	if fromAddr == "" {
		fromAddr = "sshmail <onboarding@resend.dev>"
	}
	return &Notifier{
		apiKey: apiKey,
		from:   fromAddr,
	}
}

type resendRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Text    string   `json:"text"`
}

// Send sends an email via Resend. Non-fatal errors are returned for logging.
func (n *Notifier) Send(to, subject, body string) error {
	payload := resendRequest{
		From:    n.from,
		To:      []string{to},
		Subject: subject,
		Text:    body,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal email payload: %w", err)
	}

	req, err := http.NewRequest("POST", "https://api.resend.com/emails", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+n.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("send email: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("resend API error: status %d", resp.StatusCode)
	}
	return nil
}
