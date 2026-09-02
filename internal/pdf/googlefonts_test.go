package pdf

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"ginvoice/internal/store"
)


func TestDownloadFamilyFonts_PartialFailureLeavesNoCache(t *testing.T) {
	origDir := os.Getenv("GINVOICE_DATA_DIR")
	ttf, err := fontFiles.ReadFile("fonts/DejaVuSans.ttf")
	if err != nil {
		t.Fatal(err)
	}
	fail700 := true
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/css2":
			w.Header().Set("Content-Type", "text/css")
			fmt.Fprintf(w, `@font-face { font-family: 'Partial'; font-weight: 400; src: url(%s/p-400.ttf); }
@font-face { font-family: 'Partial'; font-weight: 700; src: url(%s/p-700.ttf); }`, srv.URL, srv.URL)
		case "/p-400.ttf":
			w.Write(ttf)
		case "/p-700.ttf":
			if fail700 {
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			}
			w.Write(ttf)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("GINVOICE_GOOGLE_FONTS_BASE_URL", srv.URL)
	dir := t.TempDir()
	t.Setenv("GINVOICE_DATA_DIR", dir)

	if err := DownloadFamilyFonts("Partial"); err == nil {
		t.Fatal("expected error when the 700 download fails")
	}
	fontDir := filepath.Join(dir, "fonts")
	entries, _ := os.ReadDir(fontDir)
	if len(entries) != 0 {
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("partial download left files behind: %v", names)
	}

	fail700 = false
	if err := DownloadFamilyFonts("Partial"); err != nil {
		t.Fatalf("retry after server fixed: %v", err)
	}
	for _, f := range []string{"partial-400.ttf", "partial-700.ttf"} {
		if _, err := os.Stat(filepath.Join(fontDir, f)); err != nil {
			t.Errorf("retry: expected %s: %v", f, err)
		}
	}

	// The successful retry swapped the global font config at this test's
	// temp dir, which is deleted on teardown. Restore the persistent data
	// dir so later render tests still find DejaVu Sans.
	os.Setenv("GINVOICE_DATA_DIR", origDir)
	_ = extractFonts()
	reloadFonts()
}

func TestDownloadFamilyFonts_RejectsNonTTF(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/css2" {
			w.Header().Set("Content-Type", "text/css")
			fmt.Fprintf(w, `@font-face { font-family: 'Woff'; font-weight: 400; src: url(%s/w-400.woff2); }`, srv.URL)
			return
		}
		w.Write([]byte("wOF2" + strings.Repeat("\x00", 100)))
	}))
	defer srv.Close()
	t.Setenv("GINVOICE_GOOGLE_FONTS_BASE_URL", srv.URL)
	dir := t.TempDir()
	t.Setenv("GINVOICE_DATA_DIR", dir)

	if err := DownloadFamilyFonts("Woff"); err == nil {
		t.Fatal("expected error for woff2 payload")
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "fonts"))
	if len(entries) != 0 {
		t.Errorf("non-TTF payload left %d files behind", len(entries))
	}
}

func TestFontCatalog_Non200NotCached(t *testing.T) {
	serveErr := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveErr {
			http.Error(w, "rate limited", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"familyMetadataList":[{"family":"Zebra"}]}`)
	}))
	defer srv.Close()
	t.Setenv("GINVOICE_GOOGLE_FONTS_BASE_URL", srv.URL)
	dir := t.TempDir()
	t.Setenv("GINVOICE_DATA_DIR", dir)

	if got := FontCatalog(); got != nil {
		t.Fatalf("500 response: FontCatalog() = %v, want nil", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "fonts-catalog.json")); !os.IsNotExist(err) {
		t.Error("error response was written to the cache")
	}
	serveErr = false
	if got := FontCatalog(); len(got) != 1 || got[0] != "Zebra" {
		t.Fatalf("after recovery: FontCatalog() = %v, want [Zebra]", got)
	}
}

func TestBuildInvoiceView_SanitizesFontNames(t *testing.T) {
	inv := store.Invoice{Client: store.Client{Name: "Acme"}}
	co := store.Company{Name: "Co"}

	v := buildInvoiceView(inv, co, TemplateConfig{FontFamily: `Evil";}`})
	if v.FontFamily != "DejaVu Sans" {
		t.Errorf("invalid FontFamily = %q, want DejaVu Sans", v.FontFamily)
	}
	v = buildInvoiceView(inv, co, TemplateConfig{FontFamily: "Inter", FontTitle: `Bad";}`})
	if v.FontTitle != "Inter" {
		t.Errorf("invalid FontTitle = %q, want fallback Inter", v.FontTitle)
	}
}

func TestReloadFonts_FailureKeepsWorkingConfig(t *testing.T) {
	t.Setenv("GINVOICE_DATA_DIR", t.TempDir())
	if _, err := fontConfig(); err != nil {
		t.Fatalf("initial fontConfig: %v", err)
	}
	// Point at a data dir whose fonts subdirectory does not exist.
	// ScanFontDirectories returns an error for a non-existent directory
	// (but silently returns an empty set for a regular file), so this is
	// the reliable way to force a reload failure.
	bad := t.TempDir()
	os.Setenv("GINVOICE_DATA_DIR", bad)
	if err := reloadFonts(); err == nil {
		t.Fatal("reloadFonts with a missing fonts dir should return an error")
	}
	if _, err := fontConfig(); err != nil {
		t.Errorf("fontConfig poisoned after failed reload: %v", err)
	}
}

func TestDownloadFamilyFonts_SavesStaticWeights(t *testing.T) {
	origDir := os.Getenv("GINVOICE_DATA_DIR")
	ttf, err := fontFiles.ReadFile("fonts/DejaVuSans.ttf")
	if err != nil {
		t.Fatal(err)
	}
	var reqs atomic.Int32
	var css2UA atomic.Value
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs.Add(1)
		if r.URL.Path == "/css2" {
			css2UA.Store(r.Header.Get("User-Agent"))
			w.Header().Set("Content-Type", "text/css")
			fmt.Fprintf(w, `@font-face {
  font-family: 'Open Sans';
  font-style: normal;
  font-weight: 400;
  font-display: swap;
  src: url(%s/fonts/OpenSans-400.ttf) format('truetype');
}
@font-face {
  font-family: 'Open Sans';
  font-style: normal;
  font-weight: 700;
  font-display: swap;
  src: url(%s/fonts/OpenSans-700.ttf) format('truetype');
}`, srv.URL, srv.URL)
			return
		}
		if r.URL.Path == "/fonts/OpenSans-400.ttf" || r.URL.Path == "/fonts/OpenSans-700.ttf" {
			w.Write(ttf)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	t.Setenv("GINVOICE_GOOGLE_FONTS_BASE_URL", srv.URL)
	t.Setenv("GINVOICE_DATA_DIR", t.TempDir())

	if err := DownloadFamilyFonts("Open Sans"); err != nil {
		t.Fatalf("DownloadFamilyFonts: %v", err)
	}
	dir := filepath.Join(dataDir(), "fonts")
	for _, f := range []string{"open-sans-400.ttf", "open-sans-700.ttf"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("expected %s written: %v", f, err)
		}
	}
	if got := css2UA.Load().(string); got != "curl/8.0.0" {
		t.Errorf("css2 UA = %q, want curl/8.0.0", got)
	}
	first := reqs.Load()
	if err := DownloadFamilyFonts("Open Sans"); err != nil {
		t.Fatalf("second DownloadFamilyFonts: %v", err)
	}
	if got := reqs.Load(); got != first {
		t.Errorf("second call made %d new requests, want 0", got-first)
	}

	// DownloadFamilyFonts swapped the global font config to point at this
	// test's temp dir, which is deleted on teardown. Restore it to the
	// persistent data dir so later render tests still find DejaVu Sans.
	os.Setenv("GINVOICE_DATA_DIR", origDir)
	_ = extractFonts()
	reloadFonts()
}

func TestDownloadFamilyFonts_UnknownFamily(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusBadRequest)
	}))
	defer srv.Close()
	t.Setenv("GINVOICE_GOOGLE_FONTS_BASE_URL", srv.URL)
	dir := t.TempDir()
	t.Setenv("GINVOICE_DATA_DIR", dir)

	err := DownloadFamilyFonts("NoSuchFont")
	if err == nil {
		t.Fatal("expected error for unknown family")
	}
	if !strings.Contains(err.Error(), "NoSuchFont") {
		t.Errorf("error %q does not name family", err)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "fonts"))
	if len(entries) != 0 {
		t.Errorf("expected no files written, got %d", len(entries))
	}
}

func TestFontCatalog_ParsesAndCaches(t *testing.T) {
	var reqs atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `)]}'
{"familyMetadataList":[{"family":"Zebra"},{"family":"Alpha"}]}`)
	}))
	defer srv.Close()
	t.Setenv("GINVOICE_GOOGLE_FONTS_BASE_URL", srv.URL)
	dir := t.TempDir()
	t.Setenv("GINVOICE_DATA_DIR", dir)

	got := FontCatalog()
	if len(got) != 2 || got[0] != "Alpha" || got[1] != "Zebra" {
		t.Fatalf("FontCatalog() = %v, want [Alpha Zebra]", got)
	}
	first := reqs.Load()
	got2 := FontCatalog()
	if reqs.Load() != first {
		t.Errorf("second call made %d new requests, want 0", reqs.Load()-first)
	}
	if len(got2) != 2 {
		t.Errorf("cached FontCatalog() = %v", got2)
	}

	os.Remove(filepath.Join(dir, "fonts-catalog.json"))
	srv.Close()
	if got3 := FontCatalog(); got3 != nil {
		t.Errorf("after cache delete + server down, FontCatalog() = %v, want nil", got3)
	}
}

func TestValidateConfig_FontNames(t *testing.T) {
	if err := ValidateConfig(`{"font_family":"Inter"}`); err != nil {
		t.Errorf("valid font_family rejected: %v", err)
	}
	err := ValidateConfig(`{"font_title":"Bad\";}"}`)
	if err == nil {
		t.Fatal("expected error for bad font_title")
	}
	if !strings.Contains(err.Error(), "font_title") {
		t.Errorf("error %q does not name font_title", err)
	}
	if err := ValidateConfig(`{"unknown_key":"x"}`); err == nil {
		t.Error("expected error for unknown key")
	}
}

func TestBuildInvoiceView_FontResolution(t *testing.T) {
	inv := store.Invoice{Client: store.Client{Name: "Acme"}}
	co := store.Company{Name: "Co"}

	v := buildInvoiceView(inv, co, TemplateConfig{})
	for _, f := range []string{v.FontFamily, v.FontTitle, v.FontDetails, v.FontBody, v.FontNotes} {
		if f != "DejaVu Sans" {
			t.Errorf("empty cfg: got %q, want DejaVu Sans", f)
		}
	}

	v = buildInvoiceView(inv, co, TemplateConfig{FontFamily: "Inter"})
	if v.FontFamily != "Inter" {
		t.Errorf("FontFamily = %q, want Inter", v.FontFamily)
	}
	for _, f := range []string{v.FontTitle, v.FontDetails, v.FontBody, v.FontNotes} {
		if f != "Inter" {
			t.Errorf("family-only: got %q, want Inter", f)
		}
	}

	v = buildInvoiceView(inv, co, TemplateConfig{FontFamily: "Inter", FontTitle: "Roboto"})
	if v.FontTitle != "Roboto" {
		t.Errorf("FontTitle = %q, want Roboto", v.FontTitle)
	}
	for _, f := range []string{v.FontFamily, v.FontDetails, v.FontBody, v.FontNotes} {
		if f != "Inter" {
			t.Errorf("override: got %q, want Inter", f)
		}
	}
}
