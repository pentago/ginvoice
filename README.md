# ginvoice

A self-hosted web app for creating, managing, and emailing PDF invoices — single static binary, SQLite storage, no external dependencies.

## Quick start

```sh
docker compose up -d --build

# Then open http://localhost:8080 in your browser
```

Data is stored in `./data/` (bind-mounted to `/data` in the container). Create it before first run:

```sh
mkdir -p ./data && chown 1000:1000 ./data
```

## Environment variables

| Variable | Default | Required | Purpose |
|----------|---------|----------|---------|
| `GINVOICE_ADDR` | `:8080` | No | Listen address |
| `GINVOICE_BASE_URL` | `` | No | Public URL (for links in emails) |
| `GINVOICE_TRUSTED_PROXY_CIDR` | `` | No | CIDR(s) of trusted reverse proxy (see Auth) |
| `GINVOICE_AUTH_HEADER` | `` | No | Header name for proxy auth (e.g. `Remote-User`) |
| `GINVOICE_RESEND_API_KEY` | `` | For email | Resend API key |
| `GINVOICE_RESEND_BASE_URL` | `https://api.resend.com` | No | Override for testing |
| `GINVOICE_FROM_EMAIL` | `` | For email | Sender email address |
| `GINVOICE_FROM_NAME` | `` | For email | Sender display name |
| `GINVOICE_ENV` | `production` | No | Environment hint |

The database path defaults to `/data/ginvoice.db` and can be changed with the `--database` flag (e.g. `--database /tmp/test.db` to run against a scratch database). The PDF archive directory (`/data/invoices`) is hardcoded.

## Email setup

Email sending uses [Resend](https://resend.com):

1. Create an account at resend.com and get an API key.
2. Set `GINVOICE_RESEND_API_KEY`, `GINVOICE_FROM_EMAIL`, and `GINVOICE_FROM_NAME`.
3. Verify your sending domain in the Resend dashboard.

Without an API key the app works fine — only the "email invoice" action returns an error.

## Auth

The app runs without authentication by default and is intended for localhost/LAN use. When you put it behind a reverse proxy (Caddy, nginx, Authelia), set `GINVOICE_TRUSTED_PROXY_CIDR` to the proxy's IP CIDR and `GINVOICE_AUTH_HEADER` to the header it sets (e.g. `Remote-User` for Authelia) to enable trusted-proxy header auth. Reverse proxy setup itself is up to you.

## Building from source

Requires Go 1.27+:

```sh
GOFLAGS=-mod=mod go run github.com/a-h/templ/cmd/templ@latest generate
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/ginvoice ./cmd/app
```
