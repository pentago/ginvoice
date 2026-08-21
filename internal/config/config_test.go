package config_test

import (
	"testing"

	"ginvoice/internal/config"
)

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("GINVOICE_ADDR", "")
	t.Setenv("GINVOICE_BASE_URL", "")
	t.Setenv("GINVOICE_TRUSTED_PROXY_CIDR", "")
	t.Setenv("GINVOICE_AUTH_HEADER", "")
	t.Setenv("GINVOICE_RESEND_API_KEY", "")
	t.Setenv("GINVOICE_RESEND_BASE_URL", "")
	t.Setenv("GINVOICE_FROM_EMAIL", "")
	t.Setenv("GINVOICE_FROM_NAME", "")
	t.Setenv("GINVOICE_ENV", "")

	// Given defaults are the only inputs.
	// When the config is loaded.
	cfg := config.Load()

	// Then each field uses its documented fallback.
	if cfg.Addr != ":8080" {
		t.Fatalf("Addr = %q, want %q", cfg.Addr, ":8080")
	}
	if cfg.BaseURL != "" {
		t.Fatalf("BaseURL = %q, want empty", cfg.BaseURL)
	}
	if cfg.TrustedProxyCIDR != "" {
		t.Fatalf("TrustedProxyCIDR = %q, want empty", cfg.TrustedProxyCIDR)
	}
	if cfg.AuthHeader != "" {
		t.Fatalf("AuthHeader = %q, want empty", cfg.AuthHeader)
	}
	if cfg.ResendAPIKey != "" {
		t.Fatalf("ResendAPIKey = %q, want empty", cfg.ResendAPIKey)
	}
	if cfg.ResendBaseURL != "https://api.resend.com" {
		t.Fatalf("ResendBaseURL = %q, want %q", cfg.ResendBaseURL, "https://api.resend.com")
	}
	if cfg.FromEmail != "" {
		t.Fatalf("FromEmail = %q, want empty", cfg.FromEmail)
	}
	if cfg.FromName != "" {
		t.Fatalf("FromName = %q, want empty", cfg.FromName)
	}
	if cfg.Env != "production" {
		t.Fatalf("Env = %q, want %q", cfg.Env, "production")
	}
}

func TestLoad_EnvOverride(t *testing.T) {
	t.Setenv("GINVOICE_ADDR", ":9000")
	t.Setenv("GINVOICE_BASE_URL", "https://example.com")
	t.Setenv("GINVOICE_TRUSTED_PROXY_CIDR", "127.0.0.1/32")
	t.Setenv("GINVOICE_AUTH_HEADER", "Remote-User")
	t.Setenv("GINVOICE_RESEND_API_KEY", "api-key")
	t.Setenv("GINVOICE_RESEND_BASE_URL", "https://resend.example.com")
	t.Setenv("GINVOICE_FROM_EMAIL", "billing@example.com")
	t.Setenv("GINVOICE_FROM_NAME", "Billing")
	t.Setenv("GINVOICE_ENV", "development")

	// Given every supported env var is set.
	// When the config is loaded.
	cfg := config.Load()

	// Then the environment overrides the defaults.
	if cfg.Addr != ":9000" {
		t.Fatalf("Addr = %q, want %q", cfg.Addr, ":9000")
	}
	if cfg.BaseURL != "https://example.com" {
		t.Fatalf("BaseURL = %q, want %q", cfg.BaseURL, "https://example.com")
	}
	if cfg.TrustedProxyCIDR != "127.0.0.1/32" {
		t.Fatalf("TrustedProxyCIDR = %q, want %q", cfg.TrustedProxyCIDR, "127.0.0.1/32")
	}
	if cfg.AuthHeader != "Remote-User" {
		t.Fatalf("AuthHeader = %q, want %q", cfg.AuthHeader, "Remote-User")
	}
	if cfg.ResendAPIKey != "api-key" {
		t.Fatalf("ResendAPIKey = %q, want %q", cfg.ResendAPIKey, "api-key")
	}
	if cfg.ResendBaseURL != "https://resend.example.com" {
		t.Fatalf("ResendBaseURL = %q, want %q", cfg.ResendBaseURL, "https://resend.example.com")
	}
	if cfg.FromEmail != "billing@example.com" {
		t.Fatalf("FromEmail = %q, want %q", cfg.FromEmail, "billing@example.com")
	}
	if cfg.FromName != "Billing" {
		t.Fatalf("FromName = %q, want %q", cfg.FromName, "Billing")
	}
	if cfg.Env != "development" {
		t.Fatalf("Env = %q, want %q", cfg.Env, "development")
	}
}
