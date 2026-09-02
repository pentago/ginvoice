package handlers

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/a-h/templ"

	"ginvoice/internal/config"
	"ginvoice/internal/pdf"
	"ginvoice/internal/store"
	"ginvoice/internal/views"
)

// CompanyHandler serves the singleton company settings page.
type CompanyHandler struct {
	DB  *sql.DB
	Cfg *config.Config
}

// ShowSettings renders GET /company — the company edit form.
func (h *CompanyHandler) ShowSettings(w http.ResponseWriter, r *http.Request) {
	c, _, err := store.GetCompany(h.DB)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	cfg := pdf.LoadConfig(c.PdfConfig)
	fonts := pdf.FontCatalog()
	templ.Handler(views.SettingsPage(c, cfg, fonts)).ServeHTTP(w, r)
}

// SaveSettings handles POST /company — validates, stores the logo if uploaded,
// and upserts the singleton row, returning the HTMX confirmation fragment.
func (h *CompanyHandler) SaveSettings(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(10 << 20); err != nil { // 10MB max
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	name := r.FormValue("name")
	if name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	// tax rate: form sends %, store as basis points
	taxPct, _ := strconv.ParseFloat(r.FormValue("default_tax_rate_pct"), 64)
	taxBPS := int64(math.Round(taxPct * 100))

	c := store.Company{
		Name:                name,
		OwnerFirstName:      strings.TrimSpace(r.FormValue("owner_first_name")),
		OwnerLastName:       strings.TrimSpace(r.FormValue("owner_last_name")),
		Website:             strings.TrimSpace(r.FormValue("website")),
		AddressLine1:        r.FormValue("address_line1"),
		AddressLine2:        r.FormValue("address_line2"),
		PostalCode:          r.FormValue("postal_code"),
		City:                r.FormValue("city"),
		State:               r.FormValue("state"),
		Country:             r.FormValue("country"),
		Email:               r.FormValue("email"),
		Phone:               r.FormValue("phone"),
		TaxID:               r.FormValue("tax_id"),
		IBAN:                r.FormValue("iban"),
		DefaultTaxRateBPS:   taxBPS,
		DefaultEmailSubject: r.FormValue("default_email_subject"),
		DefaultEmailBody:    r.FormValue("default_email_body"),
		InvoiceNotes:        r.FormValue("invoice_notes"),
	}

	cfg, err := parsePdfConfigForm(r)
	if err != nil {
		// 200 so htmx swaps in the form with the error (htmx ignores 4xx by default).
		// cfg holds the partially-parsed values so the user's input is preserved.
		templ.Handler(views.SettingsForm(c, cfg, pdf.FontCatalog(), err.Error())).ServeHTTP(w, r)
		return
	}

	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	c.PdfConfig = string(cfgJSON)

	// handle logo upload; keep existing logo when no new file is posted
	existing, _, _ := store.GetCompany(h.DB)
	c.LogoData = existing.LogoData

	if err := pdf.ValidateConfig(c.PdfConfig); err != nil {
		// 200 so htmx swaps in the form with the error (htmx ignores 4xx by default)
		templ.Handler(views.SettingsForm(c, cfg, pdf.FontCatalog(), err.Error())).ServeHTTP(w, r)
		return
	}

	// download any distinct non-empty font families before saving
	seen := map[string]bool{}
	for _, fam := range []string{cfg.FontFamily, cfg.FontTitle, cfg.FontDetails, cfg.FontBody, cfg.FontNotes} {
		fam = strings.TrimSpace(fam)
		if fam == "" || seen[fam] {
			continue
		}
		seen[fam] = true
		if err := pdf.DownloadFamilyFonts(fam); err != nil {
			templ.Handler(views.SettingsForm(c, cfg, pdf.FontCatalog(), err.Error())).ServeHTTP(w, r)
			return
		}
	}

	file, _, err := r.FormFile("logo")
	if err == nil {
		defer file.Close()
		logoBytes, err := io.ReadAll(file)
		if err != nil {
			http.Error(w, "failed to read logo", http.StatusBadRequest)
			return
		}
		mimeType := http.DetectContentType(logoBytes)
		if !strings.HasPrefix(mimeType, "image/") {
			http.Error(w, "logo must be an image", http.StatusBadRequest)
			return
		}
		c.LogoData = "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(logoBytes)
	}

	if err := store.UpsertCompany(h.DB, c); err != nil {
		http.Error(w, "save failed", http.StatusInternalServerError)
		return
	}

	saved, _, _ := store.GetCompany(h.DB)
	templ.Handler(views.SettingsSaved(saved, pdf.LoadConfig(saved.PdfConfig), pdf.FontCatalog())).ServeHTTP(w, r)
}

// parsePdfConfigForm reads the PDF style form fields into a TemplateConfig.
// Empty color/numeric fields fall back to the defaults (except
// table_header_bg, where empty = transparent); unparseable numbers return an
// error naming the offending field.
func parsePdfConfigForm(r *http.Request) (pdf.TemplateConfig, error) {
	def := pdf.DefaultConfig()
	// strOr falls back to the default color when the field was not submitted
	// (e.g. a partial POST); colors have no meaningful "empty" state, unlike
	// table_header_bg where empty = transparent.
	strOr := func(v, d string) string {
		if v == "" {
			return d
		}
		return v
	}
	cfg := pdf.TemplateConfig{
		AccentColor:      strOr(strings.TrimSpace(r.FormValue("accent_color")), def.AccentColor),
		TextColor:        strOr(strings.TrimSpace(r.FormValue("text_color")), def.TextColor),
		MutedColor:       strOr(strings.TrimSpace(r.FormValue("muted_color")), def.MutedColor),
		DividerColor:     strOr(strings.TrimSpace(r.FormValue("divider_color")), def.DividerColor),
		TableHeaderBg:    strings.TrimSpace(r.FormValue("table_header_bg")),
		TableHeaderColor: strOr(strings.TrimSpace(r.FormValue("table_header_color")), def.TableHeaderColor),
		FontFamily:       strings.TrimSpace(r.FormValue("font_family")),
		FontTitle:        strings.TrimSpace(r.FormValue("font_title")),
		FontDetails:      strings.TrimSpace(r.FormValue("font_details")),
		FontBody:         strings.TrimSpace(r.FormValue("font_body")),
		FontNotes:        strings.TrimSpace(r.FormValue("font_notes")),
		ShowNotes:        r.FormValue("show_notes") == "on",
	}
	var err error
	if cfg.HeadingSize, err = parseFloatField(r, "heading_size", def.HeadingSize); err != nil {
		return cfg, err
	}
	if cfg.BodySize, err = parseFloatField(r, "body_size", def.BodySize); err != nil {
		return cfg, err
	}
	if cfg.LabelSize, err = parseFloatField(r, "label_size", def.LabelSize); err != nil {
		return cfg, err
	}
	if cfg.MarginMM, err = parseFloatField(r, "margin_mm", def.MarginMM); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// parseFloatField parses a numeric form field, returning def when empty.
func parseFloatField(r *http.Request, name string, def float64) (float64, error) {
	v := strings.TrimSpace(r.FormValue(name))
	if v == "" {
		return def, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %q is not a valid number", name, v)
	}
	return f, nil
}
