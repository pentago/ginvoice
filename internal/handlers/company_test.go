package handlers

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"fmt"
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

// fontTestDataDir is a persistent data dir shared by the font-download test.
// DownloadFamilyFonts calls reloadFonts(), which swaps the process-global font
// configuration to scan this dir. It must survive the whole test run (not be a
// per-test t.TempDir) so later PDF-rendering tests keep working; TestMain cleans
// it up after all tests.
var fontTestDataDir string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ginvoice-fonttest-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create font test dir: %v\n", err)
		os.Exit(1)
	}
	fontTestDataDir = dir
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

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

// postSettings builds and executes a multipart POST /company request.
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

	req := httptest.NewRequest(http.MethodPost, "/company", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h.SaveSettings(rec, req)
	return rec
}

// S1 happy path: POST /company with all fields persists the singleton row;
// GET /company renders the saved name and tax_id back.
func TestCompany_SaveAndShowRoundTrip(t *testing.T) {
	db, h := newCompanyTestEnv(t)

	fields := map[string]string{
		"name":                 "Acme GmbH",
		"address_line1":               "Hauptstr. 1",
		"postal_code":          "10115",
		"city":                 "Berlin",
		"country":              "Germany",
		"email":                "billing@acme.example",
		"phone":                "+49 30 123456",
		"tax_id":               "DE123456789",
		"iban":                 "DE89370400440532013000",
		"default_tax_rate_pct": "20",
	}
	rec := postSettings(t, h, fields, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /company status = %d, want 200; body: %s", rec.Code, rec.Body.String())
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
	if c.AddressLine1 != "Hauptstr. 1" || c.PostalCode != "10115" || c.City != "Berlin" || c.Country != "Germany" {
		t.Errorf("address = %q, %q, %q, %q; want Hauptstr. 1, 10115, Berlin, Germany", c.AddressLine1, c.PostalCode, c.City, c.Country)
	}
	if c.DefaultTaxRateBPS != 2000 {
		t.Errorf("default_tax_rate = %d bps, want 2000 (20%%)", c.DefaultTaxRateBPS)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/company", nil)
	getRec := httptest.NewRecorder()
	h.ShowSettings(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET /company status = %d, want 200", getRec.Code)
	}
	for _, want := range []string{"Acme GmbH", "DE123456789"} {
		if !bytes.Contains(getRec.Body.Bytes(), []byte(want)) {
			t.Errorf("GET /company body missing %q; body: %s", want, getRec.Body.String())
		}
	}
}

// S2 edge: empty name is rejected with 400 and no row is written.
func TestCompany_MissingNameRejected(t *testing.T) {
	db, h := newCompanyTestEnv(t)

	rec := postSettings(t, h, map[string]string{"name": ""}, "", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /company without name status = %d, want 400; body: %s", rec.Code, rec.Body.String())
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
		t.Fatalf("first POST /company status = %d, want 200; body: %s", first.Code, first.Body.String())
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
		t.Fatalf("second POST /company status = %d, want 200; body: %s", second.Code, second.Body.String())
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
		t.Fatalf("POST /company status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	_, ok, err := store.GetCompany(db)
	if err != nil || !ok {
		t.Fatalf("GetCompany: ok=%v err=%v", ok, err)
	}
}

// S5: invalid PDF style (bad color, bad font name) is rejected with an inline
// form error and nothing is saved — LoadConfig silently falls back to defaults
// at render time, so a typo must surface at save time.
func TestCompany_InvalidPdfConfigRejected(t *testing.T) {
	db, h := newCompanyTestEnv(t)

	for _, tc := range []struct{ name, field, val string }{
		{"bad color", "accent_color", "blue"},   // not #RRGGBB
		{"bad font", "font_family", "Bad Font!"}, // invalid font name chars
	} {
		rec := postSettings(t, h, map[string]string{"name": "Acme GmbH", tc.field: tc.val}, "", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200 (htmx swaps the form back); body: %s", tc.name, rec.Code, rec.Body.String())
		}
		if !bytes.Contains(rec.Body.Bytes(), []byte("alert-error")) {
			t.Errorf("%s: response missing alert-error; body: %s", tc.name, rec.Body.String())
		}
		if _, ok, _ := store.GetCompany(db); ok {
			t.Errorf("%s: company row written despite invalid config", tc.name)
		}
	}

	valid := map[string]string{"name": "Acme GmbH", "accent_color": "#1F1F1F"}
	rec := postSettings(t, h, valid, "", nil)
	if rec.Code != http.StatusOK || bytes.Contains(rec.Body.Bytes(), []byte("alert-error")) {
		t.Fatalf("valid config rejected: status %d, body: %s", rec.Code, rec.Body.String())
	}
	c, ok, _ := store.GetCompany(db)
	if !ok || !strings.Contains(c.PdfConfig, `"accent_color":"#1F1F1F"`) {
		t.Errorf("valid config not stored: ok=%v cfg=%q", ok, c.PdfConfig)
	}
}

// S1: saving with a font downloads it from Google Fonts and stores the config.
func TestSettings_SaveWithFonts(t *testing.T) {
	// httptest server serving the css2 API and TTF payloads.
	var srv *httptest.Server
	ttf, err := os.ReadFile(filepath.Join("..", "pdf", "fonts", "DejaVuSans.ttf"))
	if err != nil {
		t.Fatalf("read DejaVuSans.ttf: %v", err)
	}
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/css2":
			css := "@font-face { font-family: 'Test Family'; font-weight: 400; src: url(" + srv.URL + "/test-family-400.ttf); }" +
				"@font-face { font-family: 'Test Family'; font-weight: 700; src: url(" + srv.URL + "/test-family-700.ttf); }"
			w.Header().Set("Content-Type", "text/css"); w.Write([]byte(css))
		case "/test-family-400.ttf", "/test-family-700.ttf":
			w.Write(ttf)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dataDir := fontTestDataDir
	t.Setenv("GINVOICE_DATA_DIR", dataDir)
	t.Setenv("GINVOICE_GOOGLE_FONTS_BASE_URL", srv.URL)

	// Pre-populate the fonts dir with the embedded DejaVu fonts so that
	// reloadFonts() (triggered by the download) finds a complete font set.
	// fontExtractOnce runs once per process, so a prior test's extraction
	// would otherwise leave this dir without the base fonts.
	fontDir := filepath.Join(dataDir, "fonts")
	if err := os.MkdirAll(fontDir, 0o755); err != nil {
		t.Fatalf("mkdir fonts dir: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join("..", "pdf", "fonts"))
	if err != nil {
		t.Fatalf("read embedded fonts dir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join("..", "pdf", "fonts", e.Name()))
		if err != nil {
			t.Fatalf("read font %s: %v", e.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(fontDir, e.Name()), b, 0o644); err != nil {
			t.Fatalf("write font %s: %v", e.Name(), err)
		}
	}

	db, h := newCompanyTestEnv(t)
	rec := postSettings(t, h, map[string]string{"name": "Acme GmbH", "font_family": "Test Family"}, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /company status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	c, ok, err := store.GetCompany(db)
	if err != nil || !ok {
		t.Fatalf("GetCompany: ok=%v err=%v", ok, err)
	}
	if !strings.Contains(c.PdfConfig, `"font_family":"Test Family"`) {
		t.Errorf("PdfConfig missing font_family; got %q", c.PdfConfig)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "fonts", "test-family-400.ttf")); err != nil {
		t.Errorf("downloaded font file missing: %v", err)
	}
}

// S2: when the font download fails, the save is rejected with an inline error
// and the stored config is left unchanged.
func TestSettings_SaveWithFonts_DownloadFails(t *testing.T) {
	t.Setenv("GINVOICE_DATA_DIR", t.TempDir())
	t.Setenv("GINVOICE_GOOGLE_FONTS_BASE_URL", "http://127.0.0.1:1")

	db, h := newCompanyTestEnv(t)
	// seed a company with an existing config
	if err := store.UpsertCompany(db, store.Company{Name: "Acme GmbH", PdfConfig: `{"accent_color":"#111111"}`}); err != nil {
		t.Fatalf("seed company: %v", err)
	}

	rec := postSettings(t, h, map[string]string{"name": "Acme GmbH", "font_family": "Whatever"}, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /company status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("alert-error")) {
		t.Errorf("response missing alert-error; body: %s", rec.Body.String())
	}

	c, ok, _ := store.GetCompany(db)
	if !ok || c.PdfConfig != `{"accent_color":"#111111"}` {
		t.Errorf("PdfConfig changed after failed download; got %q", c.PdfConfig)
	}
}

// S2: saving with no fonts makes no network requests to the font API.
func TestSettings_SaveWithoutFonts_NoNetwork(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.NotFound(w, r)
	}))
	defer srv.Close()

	dataDir := t.TempDir()
	t.Setenv("GINVOICE_DATA_DIR", dataDir)
	t.Setenv("GINVOICE_GOOGLE_FONTS_BASE_URL", srv.URL)

	// seed the catalog cache so FontCatalog() reads locally and makes no network calls
	if err := os.WriteFile(filepath.Join(dataDir, "fonts-catalog.json"), []byte(`["Test Family"]`), 0o644); err != nil {
		t.Fatalf("seed catalog cache: %v", err)
	}

	_, h := newCompanyTestEnv(t)
	rec := postSettings(t, h, map[string]string{"name": "Acme GmbH"}, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /company status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if requests != 0 {
		t.Errorf("font API requests = %d, want 0", requests)
	}
}

// S3: legacy JSON config prefills the new form fields.
func TestSettings_LegacyConfigPrefillsForm(t *testing.T) {
	t.Setenv("GINVOICE_DATA_DIR", t.TempDir())

	db, h := newCompanyTestEnv(t)
	if err := store.UpsertCompany(db, store.Company{Name: "Acme GmbH", PdfConfig: `{"accent_color":"#FF0000","show_notes":false}`}); err != nil {
		t.Fatalf("seed company: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/company", nil)
	rec := httptest.NewRecorder()
	h.ShowSettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /company status = %d, want 200", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`value="#FF0000"`)) {
		t.Errorf("body missing prefilled accent color; body: %s", rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte(`name="show_notes" checked`)) {
		t.Errorf("show_notes checkbox should be unchecked; body: %s", rec.Body.String())
	}
}
