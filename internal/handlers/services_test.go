package handlers

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"ginvoice/internal/store"
)

// newServicesTestEnv boots a fresh temp-file SQLite DB with migrations applied
// and a mux wired exactly like cmd/app/main.go registers the services routes.
func newServicesTestEnv(t *testing.T) (*sql.DB, http.Handler) {
	t.Helper()

	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := store.RunMigrations(db, os.DirFS(filepath.Join("..", "..", "migrations"))); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	mux := http.NewServeMux()
	sh := &ServicesHandler{DB: db}
	mux.HandleFunc("GET /services", sh.List)
	mux.HandleFunc("GET /services/new", sh.New)
	mux.HandleFunc("POST /services", sh.Create)
	mux.HandleFunc("GET /services/{id}/edit", sh.Edit)
	mux.HandleFunc("POST /services/{id}", sh.Update)
	mux.HandleFunc("POST /services/{id}/delete", sh.Delete)

	return db, &servicesTestMux{Handler: mux, db: db}
}

func servicePostForm(t *testing.T, mux http.Handler, target string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func serviceGet(mux http.Handler, target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func createService(t *testing.T, mux http.Handler, name, price string) int64 {
	t.Helper()
	rec := servicePostForm(t, mux, "/services", url.Values{
		"name":       {name},
		"unit_price": {price},
		"unit":       {"hour"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create service: status = %d, want 303; body: %s", rec.Code, rec.Body.String())
	}
	return listSingleServiceID(t, mux)
}

func listSingleServiceID(t *testing.T, mux http.Handler) int64 {
	t.Helper()
	db, _ := envFromMux(mux)
	services, err := store.ListServices(db)
	if err != nil {
		t.Fatalf("list services: %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("services count = %d, want 1", len(services))
	}
	return services[0].ID
}

// envFromMux recovers the DB handle stashed on the mux wrapper. The test-only
// mux type keeps helpers terse without globals.
type servicesTestMux struct {
	http.Handler
	db *sql.DB
}

func envFromMux(mux http.Handler) (*sql.DB, bool) {
	if tm, ok := mux.(*servicesTestMux); ok {
		return tm.db, true
	}
	return nil, false
}

// S1 (happy path): creating a service with price "19.99" stores 1999 integer cents.
func TestServices_CreateStoresCents(t *testing.T) {
	db, mux := newServicesTestEnv(t)

	rec := servicePostForm(t, mux, "/services", url.Values{
		"name":        {"Web Design"},
		"description": {"Landing page design"},
		"unit":        {"hour"},
		"unit_price":  {"19.99"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /services: status = %d, want 303; body: %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/services" {
		t.Errorf("Location = %q, want /services", loc)
	}

	got, err := store.GetService(db, listSingleServiceID(t, mux))
	if err != nil {
		t.Fatalf("get service: %v", err)
	}
	if got.Name != "Web Design" {
		t.Errorf("name = %q, want %q", got.Name, "Web Design")
	}
	if got.DefaultUnitPrice != 1999 {
		t.Errorf("DefaultUnitPrice = %d, want 1999 cents (price \"19.99\" must round-trip to integer cents)", got.DefaultUnitPrice)
	}
	if got.Unit != "hour" {
		t.Errorf("unit = %q, want %q", got.Unit, "hour")
	}
}

// S2 (round-trip): the stored cents value is displayed back as a decimal price.
func TestServices_EditFormDisplaysDecimalPrice(t *testing.T) {
	_, mux := newServicesTestEnv(t)
	id := createService(t, mux, "Web Design", "19.99")

	rec := serviceGet(mux, "/services")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /services: status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Web Design") {
		t.Errorf("list page missing service name %q; body:\n%s", "Web Design", body)
	}
	if !strings.Contains(body, "19.99") {
		t.Errorf("list page missing decimal price %q; body:\n%s", "19.99", body)
	}

	rec = serviceGet(mux, "/services/"+strconv.FormatInt(id, 10)+"/edit")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /services/%d/edit: status = %d, want 200", id, rec.Code)
	}
	body = rec.Body.String()
	if !strings.Contains(body, `value="19.99"`) {
		t.Errorf("edit form missing value=\"19.99\" price round-trip; body:\n%s", body)
	}
}

// S5 (regression): update rewrites fields and re-parses the decimal price.
func TestServices_UpdateRoundTrip(t *testing.T) {
	db, mux := newServicesTestEnv(t)
	id := createService(t, mux, "Web Design", "19.99")

	rec := servicePostForm(t, mux, "/services/"+strconv.FormatInt(id, 10), url.Values{
		"name":        {"Web Design Pro"},
		"description": {"Updated"},
		"unit":        {"day"},
		"unit_price":  {"25.50"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /services/%d: status = %d, want 303; body: %s", id, rec.Code, rec.Body.String())
	}

	got, err := store.GetService(db, id)
	if err != nil {
		t.Fatalf("get service after update: %v", err)
	}
	if got.Name != "Web Design Pro" {
		t.Errorf("name = %q, want %q", got.Name, "Web Design Pro")
	}
	if got.DefaultUnitPrice != 2550 {
		t.Errorf("DefaultUnitPrice = %d, want 2550 cents (\"25.50\")", got.DefaultUnitPrice)
	}
	if got.Unit != "day" {
		t.Errorf("unit = %q, want %q", got.Unit, "day")
	}
}

// S3 (edge): deleting an unreferenced service removes it.
func TestServices_DeleteUnreferenced(t *testing.T) {
	db, mux := newServicesTestEnv(t)
	id := createService(t, mux, "Consulting", "120.00")

	rec := servicePostForm(t, mux, "/services/"+strconv.FormatInt(id, 10)+"/delete", nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST delete: status = %d, want 303; body: %s", rec.Code, rec.Body.String())
	}

	services, err := store.ListServices(db)
	if err != nil {
		t.Fatalf("list services: %v", err)
	}
	if len(services) != 0 {
		t.Errorf("services count = %d, want 0 after delete", len(services))
	}
}

// S4 (edge): deleting a service referenced by an invoice_line is blocked with 409
// and the row survives untouched.
func TestServices_DeleteBlockedWhenReferenced(t *testing.T) {
	db, mux := newServicesTestEnv(t)
	id := createService(t, mux, "Consulting", "120.00")

	seedInvoiceLineForService(t, db, id)

	rec := servicePostForm(t, mux, "/services/"+strconv.FormatInt(id, 10)+"/delete", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("POST delete referenced service: status = %d, want 409; body: %s", rec.Code, rec.Body.String())
	}

	if _, err := store.GetService(db, id); err != nil {
		t.Errorf("referenced service must survive a blocked delete, got error: %v", err)
	}
}

// Extra edge: deleting a non-existent service is 404.
func TestServices_DeleteMissing(t *testing.T) {
	_, mux := newServicesTestEnv(t)

	rec := servicePostForm(t, mux, "/services/999/delete", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("POST delete missing service: status = %d, want 404", rec.Code)
	}
}

// Extra edge: blank name is rejected with 400 and nothing is written.
func TestServices_CreateRequiresName(t *testing.T) {
	db, mux := newServicesTestEnv(t)

	rec := servicePostForm(t, mux, "/services", url.Values{"unit_price": {"10.00"}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /services without name: status = %d, want 400", rec.Code)
	}

	services, err := store.ListServices(db)
	if err != nil {
		t.Fatalf("list services: %v", err)
	}
	if len(services) != 0 {
		t.Errorf("services count = %d, want 0 (rejected create must not write)", len(services))
	}
}

// parseCents unit coverage: decimal strings -> integer cents, garbage -> 0.
func TestParseCents(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"19.99", 1999},
		{"20", 2000},
		{"0.10", 10},
		{"0.005", 1},   // rounds half away from zero
		{"19.995", 2000},
		{"garbage", 0},
		{"", 0},
	}
	for _, tc := range cases {
		if got := parseCents(tc.in); got != tc.want {
			t.Errorf("parseCents(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// seedInvoiceLineForService inserts client + invoice + one invoice_line row that
// references the given service directly via SQL (the T7 invoice flow does not exist yet).
func seedInvoiceLineForService(t *testing.T, db *sql.DB, serviceID int64) {
	t.Helper()

	res, err := db.Exec("INSERT INTO clients (name) VALUES ('Test Client')")
	if err != nil {
		t.Fatalf("seed client: %v", err)
	}
	clientID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("client last insert id: %v", err)
	}

	res, err = db.Exec(
		"INSERT INTO invoices (number, client_id, issue_date, currency) VALUES ('INV-2026-001', ?, '2026-01-01', 'EUR')",
		clientID,
	)
	if err != nil {
		t.Fatalf("seed invoice: %v", err)
	}
	invoiceID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("invoice last insert id: %v", err)
	}

	if _, err := db.Exec(
		"INSERT INTO invoice_lines (invoice_id, service_id, description, quantity, unit_price, line_total) VALUES (?, ?, 'Consulting', 2, 12000, 24000)",
		invoiceID, serviceID,
	); err != nil {
		t.Fatalf("seed invoice line: %v", err)
	}
}

