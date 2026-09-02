package handlers

import (
	"database/sql"
	"encoding/base64"
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
	templ.Handler(views.SettingsPage(c)).ServeHTTP(w, r)
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
		PdfConfig:           r.FormValue("pdf_config"),
	}

	// handle logo upload; keep existing logo when no new file is posted
	existing, _, _ := store.GetCompany(h.DB)
	c.LogoData = existing.LogoData

	if err := pdf.ValidateConfig(c.PdfConfig); err != nil {
		// 200 so htmx swaps in the form with the error (htmx ignores 4xx by default)
		templ.Handler(views.SettingsForm(c, err.Error())).ServeHTTP(w, r)
		return
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
	templ.Handler(views.SettingsSaved(saved)).ServeHTTP(w, r)
}
