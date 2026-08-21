package handlers

import (
	"database/sql"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"errors"

	"ginvoice/internal/store"
)

// newClientsTestServer spins up a real HTTP server (httptest) backed by a
// fresh temp-file SQLite DB with migrations applied, and registers the
// clients routes exactly like cmd/app/main.go does.
func newClientsTestServer(t *testing.T) (*httptest.Server, *sql.DB) {
	t.Helper()

	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	migrationsFS := os.DirFS(filepath.Join("..", "..", "migrations"))
	if err := store.RunMigrations(db, fs.FS(migrationsFS)); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	mux := http.NewServeMux()
	ch := &ClientsHandler{DB: db}
	mux.HandleFunc("GET /clients", ch.List)
	mux.HandleFunc("GET /clients/new", ch.New)
	mux.HandleFunc("POST /clients", ch.Create)
	mux.HandleFunc("GET /clients/{id}/edit", ch.Edit)
	mux.HandleFunc("POST /clients/{id}", ch.Update)
	mux.HandleFunc("POST /clients/{id}/delete", ch.Delete)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, db
}

func postForm(t *testing.T, url string, data url.Values) *http.Response {
	t.Helper()
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	body := strings.NewReader(data.Encode())
	req, err := http.NewRequest(http.MethodPost, url, body)
	if err != nil {
		t.Fatalf("new POST request %s: %v", url, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func getBody(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	var sb strings.Builder
	if _, err := io.Copy(&sb, resp.Body); err != nil {
		t.Fatalf("read body of %s: %v", url, err)
	}
	return resp.StatusCode, sb.String()
}

func createClient(t *testing.T, srv *httptest.Server, db *sql.DB, name, email string) store.Client {
	t.Helper()
	resp := postForm(t, srv.URL+"/clients", url.Values{
		"name":  {name},
		"email": {email},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create client: status = %d, want %d", resp.StatusCode, http.StatusSeeOther)
	}
	if loc := resp.Header.Get("Location"); loc != "/clients" {
		t.Fatalf("create client: Location = %q, want /clients", loc)
	}
	clients, err := store.ListClients(db)
	if err != nil {
		t.Fatalf("list clients after create: %v", err)
	}
	if len(clients) != 1 {
		t.Fatalf("clients count after create = %d, want 1", len(clients))
	}
	return clients[0]
}

// S1+S2+S3: create -> appears in list; edit email -> updated; delete -> removed.
func TestClientsCRUDRoundTrip(t *testing.T) {
	srv, db := newClientsTestServer(t)

	// S1: create client -> appears in list.
	c := createClient(t, srv, db, "Ada Lovelace", "ada@example.com")
	status, body := getBody(t, srv.URL+"/clients")
	if status != http.StatusOK {
		t.Fatalf("GET /clients: status = %d, want %d", status, http.StatusOK)
	}
	if !strings.Contains(body, "Ada Lovelace") || !strings.Contains(body, "ada@example.com") {
		t.Errorf("GET /clients body does not contain created client name/email")
	}

	// S2: edit client email -> updated.
	editStatus, editBody := getBody(t, srv.URL+"/clients/"+itoa(c.ID)+"/edit")
	if editStatus != http.StatusOK {
		t.Fatalf("GET /clients/%d/edit: status = %d, want %d", c.ID, editStatus, http.StatusOK)
	}
	if !strings.Contains(editBody, "ada@example.com") {
		t.Errorf("edit form not pre-filled with current email")
	}

	updResp := postForm(t, srv.URL+"/clients/"+itoa(c.ID), url.Values{
		"name":         {"Ada Lovelace"},
		"company_name": {"Analytical Engines Ltd"},
		"email":        {"ada@lovelace.dev"},
	})
	if updResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("update client: status = %d, want %d", updResp.StatusCode, http.StatusSeeOther)
	}
	got, err := store.GetClient(db, c.ID)
	if err != nil {
		t.Fatalf("get client after update: %v", err)
	}
	if got.Email != "ada@lovelace.dev" || got.CompanyName != "Analytical Engines Ltd" {
		t.Errorf("client after update = %+v, want updated email/company", got)
	}
	_, listBody := getBody(t, srv.URL+"/clients")
	if !strings.Contains(listBody, "ada@lovelace.dev") {
		t.Errorf("list page does not show updated email")
	}

	// S3: delete client -> removed from list and DB.
	delReq, err := http.NewRequest(http.MethodPost, srv.URL+"/clients/"+itoa(c.ID)+"/delete", nil)
	if err != nil {
		t.Fatalf("new delete request: %v", err)
	}
	delReq.Header.Set("HX-Request", "true") // HTMX row swap path
	delResp, err := http.DefaultClient.Do(delReq)
	if err != nil {
		t.Fatalf("delete client: %v", err)
	}
	defer delResp.Body.Close()
	if delResp.StatusCode != http.StatusOK {
		t.Fatalf("delete client: status = %d, want %d", delResp.StatusCode, http.StatusOK)
	}
	if _, err := store.GetClient(db, c.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("GetClient after delete: err = %v, want sql.ErrNoRows", err)
	}
	_, listBody = getBody(t, srv.URL+"/clients")
	if strings.Contains(listBody, "Ada Lovelace") {
		t.Errorf("list page still shows deleted client")
	}
}

// S4: deleting a client referenced by an invoice must return 409 and keep the client.
func TestClientsDeleteBlockedByInvoice(t *testing.T) {
	srv, db := newClientsTestServer(t)

	c := createClient(t, srv, db, "Grace Hopper", "grace@example.com")

	// Simulate the constraint: insert an invoice referencing the client directly in the DB.
	_, err := db.Exec(
		`INSERT INTO invoices(number, client_id, issue_date, currency) VALUES ('INV-2026-001', ?, '2026-01-01', 'EUR')`,
		c.ID,
	)
	if err != nil {
		t.Fatalf("seed invoice: %v", err)
	}

	delResp := postForm(t, srv.URL+"/clients/"+itoa(c.ID)+"/delete", nil)
	if delResp.StatusCode != http.StatusConflict {
		t.Fatalf("delete referenced client: status = %d, want %d", delResp.StatusCode, http.StatusConflict)
	}
	if _, err := store.GetClient(db, c.ID); err != nil {
		t.Errorf("referenced client was deleted: %v", err)
	}
	_, listBody := getBody(t, srv.URL+"/clients")
	if !strings.Contains(listBody, "Grace Hopper") {
		t.Errorf("list page no longer shows the referenced client")
	}
}

// Edge: creating a client without a name is rejected with 400 and writes nothing.
func TestClientsCreateRequiresName(t *testing.T) {
	srv, db := newClientsTestServer(t)

	resp := postForm(t, srv.URL+"/clients", url.Values{"email": {"noname@example.com"}})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("create client without name: status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	clients, err := store.ListClients(db)
	if err != nil {
		t.Fatalf("list clients: %v", err)
	}
	if len(clients) != 0 {
		t.Errorf("clients written despite missing name: %+v", clients)
	}
}

// Edge: unknown or malformed ids yield 404, not 500.
func TestClientsUnknownIDReturns404(t *testing.T) {
	srv, _ := newClientsTestServer(t)

	for _, tc := range []struct{ name, method, path string }{
		{"get-missing-edit", http.MethodGet, "/clients/999/edit"},
		{"post-missing-update", http.MethodPost, "/clients/999"},
		{"post-missing-delete", http.MethodPost, "/clients/999/delete"},
		{"bad-id-update", http.MethodPost, "/clients/notanumber"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, srv.URL+tc.path, nil)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("%s %s: %v", tc.method, tc.path, err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("%s %s: status = %d, want %d", tc.method, tc.path, resp.StatusCode, http.StatusNotFound)
			}
		})
	}
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
