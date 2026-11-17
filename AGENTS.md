# Repository Guidelines

## Project Overview

ginvoice is a self-hosted invoicing app for one company: clients, services (price list), invoices, PDF export, and emailing invoices through Resend. It ships as a single static Go binary with SQLite and server-rendered templ + HTMX pages. There are no user accounts; auth is an optional trusted-proxy header check.

## Architecture & Data Flow

```text
request → cmd/app/main.go mux
            ├─ GET /healthz                         (public)
            └─ middleware.Auth → handlers.*Handler → store.* (*sql.DB)
                                       ├─ views.*  (templ: full page, or fragment for HTMX)
                                       ├─ pdf.RenderInvoiceWithConfig → go-weasyprint
                                       └─ email.Sender.Send (Resend, PDF attached)
```

- **Wiring**: `main.go` registers every route (Go 1.22 method patterns) and builds handler structs as literals with their dependencies (`DB`, `Cfg`, `Sender`). There's no DI framework, and the only globals are the PDF render and font state. `/static/` is behind auth too. `GET /` → 302 `/invoices`; unknown paths return 404.
- **Startup**: `/app healthcheck` exits early. Otherwise: `--database` (default `/data/ginvoice.db`) → `config.Load()` → `store.Open` (pragmas `foreign_keys`, `busy_timeout(5000)`, WAL) → goose migrations from the embedded `migrations/`.
- **Store**: package-level functions on `*sql.DB` (`ListX/GetX/CreateX/UpdateX/DeleteX`, `GetCompany/UpsertCompany`), plus `store.InTx` for transactions. `companies` is a singleton row (`id=1`).
- **Invoice lifecycle**: invoices are created as `draft`. `SendEmail` sets `sent` + `sent_at` only after Resend succeeds. `Edit`/`Update` return 409 for non-drafts (`store.UpdateInvoice` also guards with `WHERE status='draft'`). Delete, batch delete and re-send work for any status. `paid` is only a list-filter value; nothing sets it.
- **Numbering**: `PREFIX-YYYY-NNN`, or `YYYY-NNN` when the client has no prefix. The sequence is `MAX(number)` per client + prefix + year (`store.NextInvoiceSeq`), but `invoices.number` is globally `UNIQUE`. `Create` retries once on a conflict. The form may send a manual `number_seq`, refreshed by `GET /invoices/next-number`.
- **Totals** are computed server-side only (`store.ComputeTotals`): line = round(qty × unit cents), discount bps on the subtotal, tax bps on subtotal − discount.
- **Line items**: the form submits only `line_service_id[i]` + `line_qty[i]`. Description and unit price are copied from the service at save time.
- **PDF**: `pdf.RenderInvoiceWithConfig(inv, company, pdf.LoadConfig(company.PdfConfig))` serves download, save (`/data/invoices/<number>.pdf`) and email. `internal/pdf/invoice.html` is a `text/template`, so nothing is auto-escaped: `buildInvoiceView` escapes every user string, and new fields must be escaped there. `/data/invoice_template.html` overrides the layout; any parse, execute or render error is logged and the built-in template is used. A global `renderMu` serializes renders.
- **PDF style**: stored as JSON in `companies.pdf_config`. Saving settings runs `parsePdfConfigForm` → `pdf.ValidateConfig` (`DisallowUnknownFields` + regexes). `pdf.LoadConfig` overlays defaults and never fails.
- **Fonts**: the embedded DejaVu TTFs are extracted once per process to `$GINVOICE_DATA_DIR/fonts` (`sync.Once`, so a failure sticks until restart). Google Fonts (static TrueType only) download synchronously inside the settings POST; if any download fails, nothing is saved. `reloadFonts` then swaps the global font config. The catalog is cached in `$GINVOICE_DATA_DIR/fonts-catalog.json` for 7 days. Invalid font names silently fall back section → family → `DejaVu Sans` (`resolveFont`).
- **Email**: `email.Sender` is an interface. `ResendSender` is a hand-written HTTP client (not the official SDK, no timeout). Subject and body fall back client → company → `email.DefaultSubject`/`DefaultBody`. `email.RenderTemplate` fills placeholders from `email.TemplateDataFor`: `{{companyName}}`, `{{companyNameLink}}`, `{{companyWebsite}}`, `{{companyURL}}`, `{{companyPhone}}`, `{{companyAddressLine1}}`, `{{companyAddressLine2}}`, `{{companyPostalCode}}`, `{{companyCity}}`, `{{companyState}}`, `{{companyCountry}}`, `{{ownerFirstName}}`, `{{ownerLastName}}`, `{{invoiceNumber}}`, `{{clientName}}`, `{{invoiceTotal}}`, `{{invoiceDueDate}}`.
- **Invoice notes**: fall back client → company → `store.DefaultInvoiceNotes`, go through `email.RenderTemplate`, then get HTML-escaped.
- **Auth** (`internal/middleware/auth.go`): with no auth env vars, everything is open. When `GINVOICE_AUTH_HEADER` is set, the peer IP (`RemoteAddr` only, never `X-Forwarded-For`) must be in `GINVOICE_TRUSTED_PROXY_CIDR` (else 403; an empty list trusts nobody), and the header must be non-empty (else 401; its value isn't checked). A CIDR without a header logs a startup warning and stays open.

## Key Directories

| Path | Purpose |
|---|---|
| `cmd/app/` | Entrypoint: flags, route registration, `healthcheck` subcommand |
| `internal/handlers/` | One file per domain (`invoices`, `clients`, `services`, `company`) + tests |
| `internal/store/` | SQLite access, models, numbering, totals |
| `internal/views/` | `.templ` sources + committed generated `*_templ.go` |
| `internal/pdf/` | HTML→PDF, embedded fonts (`fonts/`), Google Fonts, PDF style config |
| `internal/email/` | Resend client, `Sender`/`FakeSender`, template variables |
| `internal/middleware/` | Trusted-proxy auth |
| `internal/config/` | `GINVOICE_*` env loading |
| `migrations/` | goose SQL, embedded via `embed.go` |
| `web/static/` | `style.css`, `htmx.min.js`, embedded via `web/staticfs.go` |

Untracked, leave alone: `data/` (the compose container's real DB and fonts; never read or modify), `bin/` (local build output), `.omo/`, `.opencode/`, `.playwright-mcp/`, `.codegraph/`, `.worktrees/` (agent worktrees, full repo copies; exclude from searches).

## Development Commands

```sh
go test ./...                                                     # all tests, ~15s
go test ./internal/handlers/ -run TestInvoice_CreateAndView       # one test
GINVOICE_DATA_DIR=$(mktemp -d) go test ./internal/handlers/ -run TestEmail_   # PDF-rendering tests run alone
gofmt -l .                                                        # CI fails on any output
go vet ./...
GOFLAGS=-mod=mod go run github.com/a-h/templ/cmd/templ generate   # after every .templ edit; go.mod-pinned templ, never @latest
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run   # CI's version; not installed locally
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
GINVOICE_DATA_DIR=/tmp/ginvoice go run ./cmd/app --database /tmp/ginvoice/ginvoice.db   # local run on :8080
docker compose up -d --build                                      # first: mkdir -p ./data && chown 1000:1000 ./data
```

There's no Makefile, no scripts and no `//go:generate`. Never point a local run at `./data`: it holds the live compose database.

## Code Conventions & Common Patterns

- **Money**: `int64` cents; tax and discount in basis points (100 = 1%). Parse decimals with `int64(math.Round(f*100))` (`parseCents` in `handlers/services.go`), never a plain `int64(f*100)`. Display with `formatCents` (views, defined in `services.templ`) or `formatMoney` (PDF, with currency).
- **Errors and logging**: the store wraps errors with `fmt.Errorf("…: %w", err)`. Handlers map `sql.ErrNoRows` → 404; anything else gets `log.Printf` + a 500 `"internal error"`. Sentinels are checked with `errors.Is` (`store.ErrClientReferenced`). Logging uses stdlib `log`, not `slog`.
- **HTTP**: POST → 303 redirect, 400 for an invalid form, 409 for editing a non-draft. HTMX requests (`HX-Request` header) get a 200 fragment; `HX-Redirect`/`HX-Trigger` aren't used. User-facing errors on HTMX endpoints return **200** with alert HTML (`writeEmailAlert`, settings form re-render), because htmx doesn't swap 4xx/5xx responses. Keep it that way.
- **Forms**: `r.FormValue` + `strings.TrimSpace`. Settings is `multipart/form-data` (logo upload): test it with `curl -F`, and use `--form-string` for values containing `{{…}}`.
- **Templ**: regenerate after every `.templ` edit and commit the `*_templ.go` files. `{{` starts a templ expression, so write literal placeholders as `{ "{{companyName}}" }`. There's no shared layout: every page renders its own `<html>` and must link `/static/style.css` and `/static/htmx.min.js` in `<head>`. Shared parts are components (`@Nav()`, `ClientForm`, `SettingsForm`). Page scripts, like the settings live email preview, live in `SettingsPage`, outside the HTMX-swapped `SettingsForm`.
- **HTMX** handles partial updates (settings save, invoice email, line items, next-number, client delete). Invoice bulk delete and the theme toggle are inline JS. There's no Alpine.js.
- **Buttons** (`web/static/style.css`): a plain `<button>` or `a.btn` is blue (save/submit). `.btn-success` is green (add/create/send), `.btn-danger` red (delete), `.btn-warning` orange (download), `.btn-secondary` grey (edit/view/cancel/back).
- **Logo**: stored as a base64 data URI in `companies.logo_data`, never on disk. It must sniff as `image/*` via `http.DetectContentType`. The PDF embeds the data URI directly.
- **Company address**: structured columns (`address_line1`, `address_line2`, `postal_code`, `city`, `state`, `country`); the legacy `address` column is unused.
- **Migrations**: add `migrations/000NN_snake_case.sql` with `-- +goose Up` and `-- +goose Down` sections. They run automatically at startup.
- **Concurrency**: app code starts no goroutines. PDF render and font state are process-global (`renderMu`, `fontConfMu`, `fontExtractOnce`).

## Important Files

- `cmd/app/main.go`: routes, wiring, `--database`, and `healthcheck`, which dials `localhost:<port of GINVOICE_ADDR>`.
- `internal/config/config.go`: env config. `GINVOICE_DATA_DIR` and `GINVOICE_GOOGLE_FONTS_BASE_URL` are read in `internal/pdf` instead.
- `internal/store/invoice.go`: totals, numbering, invoice CRUD. `internal/store/store.go`: open, migrations, `InTx`.
- `internal/handlers/invoices.go` is the largest handler (create/edit/PDF/save/email/delete). `internal/handlers/company.go` covers settings, logo, PDF style and font downloads.
- `internal/pdf/render.go` + `invoice.html` hold the PDF view model and layout. Also see `fonts.go`, `googlefonts.go`, `config.go`.
- `internal/email/template.go`: template variables, `DefaultSubject`/`DefaultBody`.
- `internal/deps/deps.go`: blank-imports templ and goose. Both are now imported directly too, so it's redundant; leave it unless asked.
- `Dockerfile`, `compose.yaml`, `.env.example`, `.github/workflows/ci.yml`, `.golangci.yml`.

## Runtime/Tooling Preferences

- **Go 1.27** (`go.mod` `go 1.27.0`). Docker uses `golang:1.27.0-alpine` + `GOTOOLCHAIN=local`; CI reads the version from `go.mod`.
- **`CGO_ENABLED=0` always**: every dependency is pure Go (`modernc.org/sqlite`, `benoitkugler/go-weasyprint`/`webrender`). Don't add CGO, headless-browser or system-library dependencies.
- **PDF engine**: must use the Pango font configuration, because the gotext engine's text drawing is unimplemented upstream. Variable fonts and TTC collections silently render invisible text.
- **Modules**: there's no `vendor/` (gitignored). `go build`/`go test` run in default readonly mode, so dependency drift fails fast. `GOFLAGS=-mod=mod` is only for `templ generate`.
- **Local tools**: only `go`/`gofmt` are installed. templ, golangci-lint and govulncheck run via `go run` (see Development Commands).
- **Env vars** (the DB path is only the `--database` flag):

| Var | Default | Notes |
|---|---|---|
| `GINVOICE_ADDR` | `:8080` | Listen address; the healthcheck reuses its port |
| `GINVOICE_DATA_DIR` | `/data` | Fonts dir and Google Fonts catalog |
| `GINVOICE_TRUSTED_PROXY_CIDR` | empty | Comma-separated CIDRs |
| `GINVOICE_AUTH_HEADER` | empty | e.g. `Remote-User` |
| `GINVOICE_RESEND_API_KEY` | empty | Empty disables email |
| `GINVOICE_RESEND_BASE_URL` | `https://api.resend.com` | |
| `GINVOICE_FROM_EMAIL`, `GINVOICE_FROM_NAME` | empty | Sent as `From: name <email>` |
| `GINVOICE_GOOGLE_FONTS_BASE_URL` | Google endpoints | Test override |
| `GINVOICE_BASE_URL`, `GINVOICE_ENV` | empty, `production` | Loaded but unused |

- **Hardcoded `/data` paths** ignore `GINVOICE_DATA_DIR`: `/data/invoice_template.html`, `/data/invoices/`, and the default DB path.
- **Docker**: a single build stage regenerates templ and builds with BuildKit cache mounts. The runtime image is `gcr.io/distroless/static-debian13:nonroot`, with no `USER`, `VOLUME` or `/tmp` setup, and `HEALTHCHECK` runs `/app healthcheck`. `docker compose build` needs buildx ≥0.17.0.
- **Compose**: `user: 1000:1000`, `read_only: true` (only `/data` is writable, no tmpfs), `cap_drop: [ALL]`, `no-new-privileges`, port `127.0.0.1:8080:8080`, `cpus: "0.1"`, `memory: 256M`. The `./data:/data` bind mount must exist and be owned by 1000:1000 before the first `up`; otherwise Docker creates it as root.
- **Secrets**: `.env` is gitignored; never print it. It feeds compose's `${RESEND_API_KEY}`, `${FROM_EMAIL}`, `${FROM_NAME}` and `${BASE_URL}`. `.env.example` is the tracked template.

## Testing & QA

- **Style**: stdlib `testing` only, with no testify and no DB mocks. Test names follow `TestSubject_Behavior`, and handler/store tests carry a `// S<n>: …` scenario comment. Tests are mostly single-case; table-driven only where inputs vary (`TestAuth`, `TestParseCents`). No `t.Parallel`.
- **Handler tests**: each test gets a real temp SQLite DB: `store.Open(filepath.Join(t.TempDir(), "test.db"))` + `store.RunMigrations(db, os.DirFS("../../migrations"))`. Handlers are built as struct literals, and routes are re-registered by hand on a test mux without auth. Helpers (`new*TestServer`/`new*TestEnv`) live in each test file; copy the closest one. A new route in `main.go` needs to be added to the test mux too.
- **Fakes**: `email.FakeSender` records calls (set `Err` to make it fail) and is injected via `InvoicesHandler.Sender`. Google Fonts is faked with an `httptest` server plus `t.Setenv("GINVOICE_GOOGLE_FONTS_BASE_URL", …)`. No test touches the network.
- **Global PDF state**: fonts are extracted once per process. PDF-rendering handler tests pass in a full run only because `TestSettings_SaveWithFonts` points `GINVOICE_DATA_DIR` at `fontTestDataDir` first. New ones should `t.Setenv("GINVOICE_DATA_DIR", fontTestDataDir)` (from `TestMain`, outlives the test), never a per-test `t.TempDir()`. Tests that call `DownloadFamilyFonts` must restore the font config afterwards (`extractFonts(); reloadFonts()`; see `pdf/googlefonts_test.go`). A `/data/invoice_template.html` on the machine changes PDF output.
- **CI** (`.github/workflows/ci.yml`, on push/PR to `main` and `v*` tags) runs:
  - `fmt`
  - `templ`: regenerate, then `git diff --quiet`. That check misses a new untracked `*_templ.go`, so commit it.
  - `vet`
  - `vulncheck`
  - `lint`: golangci-lint v2.14.0, standard linters, `only-new-issues: true`
  - `test`: `go test ./... -count=1`
  - `build`
  - `docker`, only on push to `main` or a `v*` tag: pushes `ghcr.io/pentago/ginvoice` with branch, semver and short-SHA tags, plus `latest` on `main`.
- **Lint baseline**: a full local golangci-lint run reports ~32 pre-existing findings (mostly errcheck on deferred `Close`), and CI fails only on new ones. Don't add more, and don't mass-fix them unless asked.
- **Coverage**: there's no coverage gate.
- **Done means**: `gofmt -l .` prints nothing, `go vet ./...` and `go test ./...` pass, and templ generate leaves no diff.
