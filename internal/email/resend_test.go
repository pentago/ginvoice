package email

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// S5: ResendSender posts JSON with Bearer auth and a base64 attachment.
func TestResendSender_SendsCorrectRequest(t *testing.T) {
	var received *http.Request
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = r
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"test"}`))
	}))
	defer srv.Close()

	sender := &ResendSender{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		From:    "billing@example.com",
	}

	pdfBytes := []byte("%PDF-1.7 test")
	err := sender.Send(context.Background(), "client@example.com", "Invoice INV-001", "<p>see attached</p>", "INV-001.pdf", pdfBytes)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got := received.Header.Get("Authorization"); got != "Bearer test-key" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer test-key")
	}

	if got := received.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json")
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}

	toArr := payload["to"].([]interface{})
	if len(toArr) != 1 || toArr[0] != "client@example.com" {
		t.Errorf("to = %v, want [client@example.com]", toArr)
	}

	if got := payload["subject"]; got != "Invoice INV-001" {
		t.Errorf("subject = %v, want Invoice INV-001", got)
	}

	if got := payload["from"]; got != "billing@example.com" {
		t.Errorf("from = %v, want billing@example.com", got)
	}

	if got := payload["html"]; got != "<p>see attached</p>" {
		t.Errorf("html = %v, want <p>see attached</p>", got)
	}

	atts := payload["attachments"].([]interface{})
	if len(atts) != 1 {
		t.Fatalf("attachments count = %d, want 1", len(atts))
	}
	att := atts[0].(map[string]interface{})
	if att["filename"] != "INV-001.pdf" {
		t.Errorf("filename = %v, want INV-001.pdf", att["filename"])
	}
	expected := base64.StdEncoding.EncodeToString(pdfBytes)
	if att["content"] != expected {
		t.Errorf("attachment content mismatch")
	}
}

// ResendSender falls back to the public API URL when BaseURL is empty.
func TestResendSender_DefaultBaseURL(t *testing.T) {
	r := &ResendSender{APIKey: "k"}
	if got := r.baseURL(); got != "https://api.resend.com" {
		t.Errorf("baseURL() = %q, want https://api.resend.com", got)
	}
	r.BaseURL = "http://localhost:1234"
	if got := r.baseURL(); got != "http://localhost:1234" {
		t.Errorf("baseURL() = %q, want http://localhost:1234", got)
	}
}

// Non-2xx responses surface as errors.
func TestResendSender_Non2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	sender := &ResendSender{APIKey: "bad", BaseURL: srv.URL}
	err := sender.Send(context.Background(), "to@x.com", "s", "<p>b</p>", "", nil)
	if err == nil {
		t.Fatal("Send with HTTP 401: err = nil, want error")
	}
}
