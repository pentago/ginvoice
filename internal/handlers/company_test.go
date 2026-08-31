package handlers

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ginvoice/internal/config"
	"ginvoice/internal/store"
)

func newCompanyTestEnv(t *testing.T) (*sql.DB, *CompanyHandler) {
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

	cfg := &config.Config{}
	h := &CompanyHandler{DB: db, Cfg: cfg}
	return db, h
}

// postSettings builds and executes a multipart POST /settings request.
func postSettings(t *testing.T, h *CompanyHandler, fields map[string]string, logoName string, logoBytes []byte) *httptest.ResponseRecorder {
	t.Helper()

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatalf("write field %s: %v", k, err)
		}
	}
	if logoName != "" {
		fw, err := mw.CreateFormFile("logo", logoName)
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, err := fw.Write(logoBytes); err != nil {
			t.Fatalf("write logo bytes: %v", err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/settings", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h.SaveSettings(rec, req)
	return rec
}

// S1 happy path: POST /settings with all fields persists the singleton row;
// GET /settings renders the saved name and tax_id back.
func TestCompany_SaveAndShowRoundTrip(t *testing.T) {
	db, h := newCompanyTestEnv(t)

	fields := map[string]string{
		"name":                 "Acme GmbH",
		"address":              "Hauptstr. 1, Berlin",
		"email":                "billing@acme.example",
		"phone":                "+49 30 123456",
		"tax_id":               "DE123456789",
		"iban":                 "DE89370400440532013000",
		"default_tax_rate_pct": "20",
	}
	rec := postSettings(t, h, fields, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /settings status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("Acme GmbH")) {
		t.Errorf("POST response missing saved confirmation name; body: %s", rec.Body.String())
	}

	c, ok, err := store.GetCompany(db)
	if err != nil {
		t.Fatalf("GetCompany: %v", err)
	}
	if !ok {
		t.Fatal("expected company row id=1 after upsert, got none")
	}
	if c.Name != "Acme GmbH" {
		t.Errorf("name = %q, want Acme GmbH", c.Name)
	}
	if c.TaxID != "DE123456789" {
		t.Errorf("tax_id = %q, want DE123456789", c.TaxID)
	}
	if c.DefaultTaxRateBPS != 2000 {
		t.Errorf("default_tax_rate = %d bps, want 2000 (20%%)", c.DefaultTaxRateBPS)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/settings", nil)
	getRec := httptest.NewRecorder()
	h.ShowSettings(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET /settings status = %d, want 200", getRec.Code)
	}
	for _, want := range []string{"Acme GmbH", "DE123456789"} {
		if !bytes.Contains(getRec.Body.Bytes(), []byte(want)) {
			t.Errorf("GET /settings body missing %q; body: %s", want, getRec.Body.String())
		}
	}
}

// S2 edge: empty name is rejected with 400 and no row is written.
func TestCompany_MissingNameRejected(t *testing.T) {
	db, h := newCompanyTestEnv(t)

	rec := postSettings(t, h, map[string]string{"name": ""}, "", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /settings without name status = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}

	_, ok, err := store.GetCompany(db)
	if err != nil {
		t.Fatalf("GetCompany: %v", err)
	}
	if ok {
		t.Error("company row written despite missing name; want no row")
	}
}

// S3 logo upload stores base64 data URI in the database, not on disk.
// Re-upload with a different image replaces the stored data.
func TestCompany_LogoReuploadReplacesData(t *testing.T) {
	db, h := newCompanyTestEnv(t)

	pngBytes := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	jpgBytes := []byte{0xff, 0xd8, 0xff, 0xe0}

	first := postSettings(t, h, map[string]string{"name": "Acme GmbH"}, "logo.png", pngBytes)
	if first.Code != http.StatusOK {
		t.Fatalf("first POST /settings status = %d, want 200; body: %s", first.Code, first.Body.String())
	}

	c1, ok, err := store.GetCompany(db)
	if err != nil || !ok {
		t.Fatalf("GetCompany after first upload: ok=%v err=%v", ok, err)
	}
	if !strings.HasPrefix(c1.LogoData, "data:image/png;base64,") {
		t.Fatalf("logo_data after first upload = %q, want data:image/png;base64,...", c1.LogoData)
	}
	// verify the base64 payload round-trips
	payload := strings.TrimPrefix(c1.LogoData, "data:image/png;base64,")
	if decoded, err := base64.StdEncoding.DecodeString(payload); err != nil || !bytes.Equal(decoded, pngBytes) {
		t.Errorf("logo_data base64 does not round-trip: decoded=%v err=%v", len(decoded), err)
	}

	second := postSettings(t, h, map[string]string{"name": "Acme GmbH"}, "logo.jpg", jpgBytes)
	if second.Code != http.StatusOK {
		t.Fatalf("second POST /settings status = %d, want 200; body: %s", second.Code, second.Body.String())
	}

	c2, _, err := store.GetCompany(db)
	if err != nil {
		t.Fatalf("GetCompany after second upload: %v", err)
	}
	if !strings.HasPrefix(c2.LogoData, "data:image/jpeg;base64,") {
		t.Errorf("logo_data after re-upload = %q, want data:image/jpeg;base64,...", c2.LogoData)
	}
}

// S4 defaults: blank currency falls back to EUR.
func TestCompany_BlankFieldsGetDefaults(t *testing.T) {
	db, h := newCompanyTestEnv(t)

	rec := postSettings(t, h, map[string]string{
		"name": "Solo Dev",
	}, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /settings status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	_, ok, err := store.GetCompany(db)
	if err != nil || !ok {
		t.Fatalf("GetCompany: ok=%v err=%v", ok, err)
	}
}
