package email

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
)

const defaultResendBaseURL = "https://api.resend.com"

// ResendSender sends email via the Resend HTTP API.
type ResendSender struct {
	APIKey  string
	BaseURL string // default: "https://api.resend.com"
	From    string // "Name <email>" or just "email"
	Client  *http.Client
}

type resendPayload struct {
	From        string             `json:"from"`
	To          []string           `json:"to"`
	Subject     string             `json:"subject"`
	HTML        string             `json:"html"`
	Attachments []resendAttachment `json:"attachments,omitempty"`
}

type resendAttachment struct {
	Filename string `json:"filename"`
	Content  string `json:"content"` // base64
}

// baseURL returns the configured API base URL or the public Resend endpoint.
func (r *ResendSender) baseURL() string {
	if r.BaseURL == "" {
		return defaultResendBaseURL
	}
	return r.BaseURL
}

func (r *ResendSender) Send(ctx context.Context, to, subject, html, attachmentName string, attachmentBytes []byte) error {
	payload := resendPayload{
		From:    r.From,
		To:      []string{to},
		Subject: subject,
		HTML:    html,
	}
	if len(attachmentBytes) > 0 {
		payload.Attachments = []resendAttachment{{
			Filename: attachmentName,
			Content:  base64.StdEncoding.EncodeToString(attachmentBytes),
		}}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL()+"/emails", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+r.APIKey)
	req.Header.Set("Content-Type", "application/json")

	client := r.Client
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("resend HTTP %d", resp.StatusCode)
	}
	return nil
}
