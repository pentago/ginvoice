@~/.config/opencode/AGENTS.md

# ginvoice

Self-hosted invoicing app. Single Go binary, SQLite, HTMX + templ, PDF export, Resend email.

## Commands

```sh
docker compose up -d --build   # build image + start container
go test ./...                  # run tests locally
go test ./internal/handlers/   # single package
go test ./internal/handlers/ -run TestInvoice_CreateAndView  # single test
```

**Always run templ generate after editing `.templ` files:** `GOFLAGS=-mod=mod go run github.com/a-h/templ/cmd/templ@latest generate`
The generated `*_templ.go` files are checked in and the build will fail or use stale code without regeneration.

Requires buildx ≥0.17.0 (BuildKit is the default builder; `# syntax=docker/dockerfile:1` directive and `--mount=type=cache` in Dockerfile need it).

## Architecture

- `cmd/app/main.go` — entrypoint, route registration, DB open + migrate. All routes wired here. DB path (`/data/ginvoice.db`) and data dir (`/data`) are hardcoded constants — not configurable via env.
- `cmd/app/main.go` healthcheck subcommand — when invoked as `/app healthcheck`, reads `GINVOICE_ADDR`, GETs `/healthz`, exits 0/1. Used by Docker HEALTHCHECK.
- `internal/config/` — env-based config (`GINVOICE_*` env vars, see `config.go`). No `DBPath` or `DataDir` fields — those are hardcoded in `main.go`.
- `internal/store/` — SQLite via `modernc.org/sqlite` (pure Go, CGO-free). Goose migrations. All money in integer cents.
- `internal/handlers/` — HTTP handlers, one file per domain (invoices, clients, services, company). Tests in `*_test.go` use httptest + real temp SQLite DB.
- `internal/views/` — templ templates (`.templ` files). Every page renders full HTML (no shared layout — `@Nav()` is the only shared component).
- `internal/pdf/` — maroto v2 + pdfcpu for PDF generation/optimization. `init()` sets `model.ConfigPath = "disable"` so pdfcpu doesn't try to create a config directory (breaks in read-only containers).
- `internal/email/` — custom Resend HTTP client (not the official SDK). `Sender` interface + `FakeSender` for tests. `template.go` has `RenderTemplate()` for `{{variable}}` substitution and `DefaultSubject`/`DefaultBody` constants.
- `internal/middleware/auth.go` — trusted-proxy header auth. Open mode by default (no auth). Three modes documented in the file.
- `migrations/` — goose SQL migrations (4 so far), embedded via `embed.go`.
- `web/static/` — CSS + htmx.min.js, embedded via `web/staticfs.go`.

## Conventions

- **Money**: all amounts are integer cents (`int64`). Tax rate is basis points (100 = 1%). No floats for money. `formatCents()` converts for display.
- **Templ**: after editing any `.templ` file, run `GOFLAGS=-mod=mod go run github.com/a-h/templ/cmd/templ@latest generate` to regenerate Go code. `{{` in templ is expression syntax — literal `{{var}}` in text must be wrapped in a string expression: `{ "{{companyName}}" }`.
- **Vendored**: `vendor/` is gitignored — builds use `GOFLAGS=-mod=mod` (module cache, not vendor). Run `go mod vendor` only if you need vendor mode locally. In Dockerfile, `-mod=mod` is scoped to `templ generate` only; `go build` uses default (readonly) so dependency drift fails fast.
- **Tests**: handler tests spin up a real httptest server with a temp SQLite DB and run migrations — no mocks for the DB layer.
- **Static binary**: `CGO_ENABLED=0` always (modernc.org/sqlite is pure Go). Docker runtime is `scratch`.
- **Settings form**: uses `multipart/form-data` (logo upload). Use `curl -F` not `curl -d` when testing settings POST. For fields containing `{{}}`, use `--form-string` not `-F` (curl interprets `{{` as a file glob).
- **Logo storage**: logo is stored as a base64 data URI string in the `logo_data` column of `companies`, not on disk. PDF renderer decodes the data URI to bytes and uses `image.NewFromBytes()`.
- **Invoice line items**: description and unit price are inherited from the selected service (not user-editable on the invoice form). Only service + quantity are submitted.
- **Sent invoices are immutable**: handlers return 409 on edit/update attempts.
- **Email templates**: company-level default subject/body in settings, per-client override in client form. Fallback chain: client template → company default → built-in `email.DefaultSubject`/`DefaultBody`. Variables: `{{companyName}}`, `{{companyNameLink}}`, `{{companyWebsite}}`, `{{companyURL}}`, `{{companyPhone}}`, `{{ownerFirstName}}`, `{{ownerLastName}}`, `{{invoiceNumber}}`, `{{clientName}}`, `{{invoiceTotal}}`, `{{invoiceDueDate}}`.
- **Button colors**: `.btn` = blue (save/submit), `.btn-success` = green (add/create), `.btn-danger` = red (delete), `.btn-warning` = orange (download), `.btn-secondary` = grey (edit/view/cancel/back).
- **Go 1.27**: `mime.DetectContentType` moved to `net/http` — use `http.DetectContentType`.
- **Secrets**: API key and email config are in `.env` (gitignored). Compose reads via `${VAR}` substitution. `.env.example` is tracked as a template.

## Gotchas

- `internal/deps/deps.go` has blank imports to keep build-time-only deps (templ, maroto, pdfcpu, goose) in go.mod. Don't remove it.
- `GET /` redirects to `/invoices`. Unknown paths under `/` return 404 (not redirected).
- Every page template must include `<link rel="stylesheet" href="/static/style.css"/>` and `<script src="/static/htmx.min.js"></script>` in `<head>` — there's no shared layout to inherit from.
- HTMX is used for: add line item (invoice form), delete buttons (clients/services), email invoice, settings save. Alpine.js was removed (was unused).
- Settings page has a live email preview (iframe + inline JS) that substitutes template variables with sample data in real-time. The script lives in `SettingsPage`, not `SettingsForm`, so it survives HTMX swaps.

## Docker

- **Non-root**: Dockerfile uses `USER 65534:65534`. Compose overrides with `user: 1000:1000`.
- **Bind mount permissions**: `compose.yaml` bind-mounts `./data:/data`. Docker creates this dir as root if it doesn't exist. Before first `docker compose up -d`, run: `mkdir -p ./data && chown 1000:1000 ./data` (or whatever UID `user:` is set to). No init container — this is a manual step.
- **Read-only root FS**: `read_only: true` in compose. `/tmp` is copied clean from the build stage (not a tmpfs mount).
- **All caps dropped**: `cap_drop: [ALL]` + `security_opt: ["no-new-privileges:true"]`.
- **No `VOLUME /data`**: removed from Dockerfile. Use bind mounts only — no anonymous named volumes.
- **BuildKit cache mounts**: Dockerfile uses `--mount=type=cache` on the build RUN for `/go/pkg/mod` and `/root/.cache/go-build`. Speeds up rebuilds; contents don't enter the image.
- **Resource limits**: compose sets `cpus: "0.1"` and `memory: 256M`.
- **Loopback only**: compose binds ports to `127.0.0.1:8080:8080`, not `0.0.0.0`.
