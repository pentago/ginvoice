package config

import "os"

type Config struct {
	Addr             string
	BaseURL          string
	TrustedProxyCIDR string
	AuthHeader       string
	ResendAPIKey     string
	ResendBaseURL    string
	FromEmail        string
	FromName         string
	Env              string
}

func Load() *Config {
	return &Config{
		Addr:             getenv("GINVOICE_ADDR", ":8080"),
		BaseURL:          getenv("GINVOICE_BASE_URL", ""),
		TrustedProxyCIDR: getenv("GINVOICE_TRUSTED_PROXY_CIDR", ""),
		AuthHeader:       getenv("GINVOICE_AUTH_HEADER", ""),
		ResendAPIKey:     getenv("GINVOICE_RESEND_API_KEY", ""),
		ResendBaseURL:    getenv("GINVOICE_RESEND_BASE_URL", "https://api.resend.com"),
		FromEmail:        getenv("GINVOICE_FROM_EMAIL", ""),
		FromName:         getenv("GINVOICE_FROM_NAME", ""),
		Env:              getenv("GINVOICE_ENV", "production"),
	}
}

func getenv(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	return value
}
