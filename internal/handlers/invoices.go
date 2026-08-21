package handlers

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"

	"ginvoice/internal/config"
	"ginvoice/internal/email"
	"ginvoice/internal/pdf"
	"ginvoice/internal/store"
	"ginvoice/internal/views"
)

// InvoicesHandler serves the invoice CRUD pages.
type InvoicesHandler struct {
	DB     *sql.DB
	Cfg    *config.Config
	Sender email.Sender
}

// List handles GET /invoices — renders the full invoices list page.
func (h *InvoicesHandler) List(w http.ResponseWriter, r *http.Request) {
	invoices, err := store.ListInvoices(h.DB)
	if err != nil {
		log.Printf("list invoices: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// Load client for each invoice for display
	for i := range invoices {
		client, err := store.GetClient(h.DB, invoices[i].ClientID)
		if err != nil {
			log.Printf("get client for invoice %d: %v", invoices[i].ID, err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		invoices[i].Client = client
	}
	templ.Handler(views.InvoicesPage(invoices)).ServeHTTP(w, r)
}

// New handles GET /invoices/new — renders a blank invoice form.
// Optional ?client_id=X pre-selects the client.
func (h *InvoicesHandler) New(w http.ResponseWriter, r *http.Request) {
	clients, err := store.ListClients(h.DB)
	if err != nil {
		log.Printf("list clients: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	services, err := store.ListServices(h.DB)
	if err != nil {
		log.Printf("list services: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Build invoice with optional client prefill and company defaults
	inv := store.Invoice{}
	company, _, _ := store.GetCompany(h.DB)
	if company.ID > 0 {
		inv.TaxRate = company.DefaultTaxRateBPS
	}
	if clientIDStr := r.URL.Query().Get("client_id"); clientIDStr != "" {
		clientID, err := strconv.ParseInt(clientIDStr, 10, 64)
		if err == nil && clientID > 0 {
			inv.ClientID = clientID
		}
	}

	templ.Handler(views.NewInvoicePage(clients, services, inv)).ServeHTTP(w, r)
}

// LineItem handles GET /invoices/line-item — returns a line-item row partial for HTMX.
func (h *InvoicesHandler) LineItem(w http.ResponseWriter, r *http.Request) {
	services, err := store.ListServices(h.DB)
	if err != nil {
		log.Printf("list services: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Get the index from query param or default to 0
	index := 0
	if indexStr := r.URL.Query().Get("index"); indexStr != "" {
		if i, err := strconv.Atoi(indexStr); err == nil {
			index = i
		}
	}

	templ.Handler(views.InvoiceLineRowPartial(index, services)).ServeHTTP(w, r)
}

// Create handles POST /invoices — validates and creates a new invoice.
func (h *InvoicesHandler) Create(w http.ResponseWriter, r *http.Request) {
	inv, lines, ok := h.parseInvoiceForm(w, r)
	if !ok {
		return
	}

	// Get company for defaults
	company, ok, err := store.GetCompany(h.DB)
	if err != nil {
		log.Printf("get company: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "set up company profile first", http.StatusBadRequest)
		return
	}

	// Set defaults from company
	inv.Currency = company.DefaultCurrency
	if inv.Currency == "" {
		inv.Currency = "EUR"
	}

	// Use tax rate from form (always provided by the form element)
	// The form shows the company default on page load; user can override.

	// Compute totals
	inv.Subtotal, inv.TaxAmount, inv.Total = store.ComputeTotals(lines, inv.TaxRate)

	// Generate invoice number
	now := time.Now()
	year := now.Year()
	prefix := company.InvoiceNumberPrefix
	if prefix == "" {
		prefix = "INV"
	}

	// Try to create invoice with unique number, retry once on conflict
	for attempt := 0; attempt < 2; attempt++ {
		num, err := store.NextInvoiceNumber(h.DB, prefix, year)
		if err != nil {
			log.Printf("next invoice number: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		inv.Number = num
		inv.Status = "draft"

		invoiceID, err := store.CreateInvoice(h.DB, inv, lines)
		if err != nil {
			// Check if it's a unique constraint violation
			if strings.Contains(err.Error(), "UNIQUE constraint") && attempt == 0 {
				continue // retry with fresh number
			}
			log.Printf("create invoice: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, fmt.Sprintf("/invoices/%d", invoiceID), http.StatusSeeOther)
		return
	}

	http.Error(w, "failed to create invoice after retry", http.StatusInternalServerError)
}

// View handles GET /invoices/{id} — renders the invoice in read-only mode.
func (h *InvoicesHandler) View(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}

	inv, err := store.GetInvoiceWithLines(h.DB, id)
	if err != nil {
		h.invoiceError(w, r, err)
		return
	}

	templ.Handler(views.InvoicePage(inv)).ServeHTTP(w, r)
}

// Edit handles GET /invoices/{id}/edit — renders the edit form for a draft invoice.
func (h *InvoicesHandler) Edit(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}

	inv, err := store.GetInvoiceWithLines(h.DB, id)
	if err != nil {
		h.invoiceError(w, r, err)
		return
	}

	if inv.Status != "draft" {
		http.Error(w, "cannot edit a sent invoice", http.StatusConflict)
		return
	}

	clients, err := store.ListClients(h.DB)
	if err != nil {
		log.Printf("list clients: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	services, err := store.ListServices(h.DB)
	if err != nil {
		log.Printf("list services: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	templ.Handler(views.EditInvoicePage(clients, services, inv)).ServeHTTP(w, r)
}

// Update handles POST /invoices/{id} — updates a draft invoice.
func (h *InvoicesHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}

	// Check invoice exists and is draft
	inv, err := store.GetInvoice(h.DB, id)
	if err != nil {
		h.invoiceError(w, r, err)
		return
	}

	if inv.Status != "draft" {
		http.Error(w, "cannot update a sent invoice", http.StatusConflict)
		return
	}

	// Parse form
	updatedInv, lines, ok := h.parseInvoiceForm(w, r)
	if !ok {
		return
	}

	// Set fields that don't come from the form
	updatedInv.ID = inv.ID
	updatedInv.Number = inv.Number
	updatedInv.Status = inv.Status
	updatedInv.Currency = inv.Currency

	// Recompute totals
	updatedInv.Subtotal, updatedInv.TaxAmount, updatedInv.Total = store.ComputeTotals(lines, updatedInv.TaxRate)

	if err := store.UpdateInvoice(h.DB, updatedInv, lines); err != nil {
		log.Printf("update invoice %d: %v", id, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/invoices/%d", id), http.StatusSeeOther)
}

// parseInvoiceForm parses the invoice form fields and line items.
func (h *InvoicesHandler) parseInvoiceForm(w http.ResponseWriter, r *http.Request) (store.Invoice, []store.InvoiceLine, bool) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return store.Invoice{}, nil, false
	}

	clientID, err := strconv.ParseInt(r.FormValue("client_id"), 10, 64)
	if err != nil || clientID <= 0 {
		http.Error(w, "client_id is required", http.StatusBadRequest)
		return store.Invoice{}, nil, false
	}

	issueDate := strings.TrimSpace(r.FormValue("issue_date"))
	if issueDate == "" {
		http.Error(w, "issue_date is required", http.StatusBadRequest)
		return store.Invoice{}, nil, false
	}

	// Parse tax rate from percentage to basis points
	taxRateBPS := int64(0)
	if taxRatePct := r.FormValue("tax_rate_pct"); taxRatePct != "" {
		if f, err := strconv.ParseFloat(taxRatePct, 64); err == nil {
			taxRateBPS = int64(f * 100)
		}
	}

	inv := store.Invoice{
		ClientID:  clientID,
		IssueDate: issueDate,
		DueDate:   strings.TrimSpace(r.FormValue("due_date")),
		Notes:     strings.TrimSpace(r.FormValue("notes")),
		TaxRate:   taxRateBPS,
	}

	// Parse line items
	var lines []store.InvoiceLine
	for i := 0; ; i++ {
		sidStr := r.FormValue(fmt.Sprintf("line_service_id[%d]", i))
		if sidStr == "" {
			break
		}

		sid, err := strconv.ParseInt(sidStr, 10, 64)
		if err != nil || sid <= 0 {
			http.Error(w, "line item service is required", http.StatusBadRequest)
			return store.Invoice{}, nil, false
		}

		svc, err := store.GetService(h.DB, sid)
		if err != nil {
			http.Error(w, "service not found", http.StatusBadRequest)
			return store.Invoice{}, nil, false
		}

		qty, _ := strconv.ParseFloat(r.FormValue(fmt.Sprintf("line_qty[%d]", i)), 64)
		if qty <= 0 {
			http.Error(w, "line item quantity must be greater than zero", http.StatusBadRequest)
			return store.Invoice{}, nil, false
		}
		unitPrice := svc.DefaultUnitPrice

		lines = append(lines, store.InvoiceLine{
			ServiceID:   &sid,
			Description: svc.Description,
			Quantity:    qty,
			UnitPrice:   unitPrice,
			SortOrder:   i,
		})
	}

	if len(lines) == 0 {
		http.Error(w, "at least one line item is required", http.StatusBadRequest)
		return store.Invoice{}, nil, false
	}

	return inv, lines, true
}

// pathID parses the {id} path value; writes 404 on malformed input.
func (h *InvoicesHandler) pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return 0, false
	}
	return id, true
}

// invoiceError maps store errors to responses: 404 for missing rows, 500 otherwise.
func (h *InvoicesHandler) invoiceError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	log.Printf("invoices handler: %v", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

// invoicePDF loads the invoice with lines plus the company profile and
// returns the optimized PDF bytes.
func (h *InvoicesHandler) invoicePDF(w http.ResponseWriter, r *http.Request, id int64) ([]byte, string, bool) {
	inv, err := store.GetInvoiceWithLines(h.DB, id)
	if err != nil {
		h.invoiceError(w, r, err)
		return nil, "", false
	}

	company, _, err := store.GetCompany(h.DB)
	if err != nil {
		log.Printf("get company for invoice pdf: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return nil, "", false
	}

	raw, err := pdf.RenderInvoice(inv, company)
	if err != nil {
		log.Printf("render invoice %d pdf: %v", id, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return nil, "", false
	}

	number := filepath.Base(inv.Number) // defensive: number is DB-generated
	return raw, number, true
}

// DownloadPDF handles GET /invoices/{id}/pdf — streams the PDF as an attachment download.
func (h *InvoicesHandler) DownloadPDF(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}

	body, number, ok := h.invoicePDF(w, r, id)
	if !ok {
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", number+".pdf"))
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(body); err != nil {
		log.Printf("write invoice %d pdf: %v", id, err)
	}
}

// SavePDF handles POST /invoices/{id}/save — writes the optimized PDF to
// /data/invoices/<number>.pdf, creating the directory and
// overwriting any existing file on re-save.
func (h *InvoicesHandler) SavePDF(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}

	body, number, ok := h.invoicePDF(w, r, id)
	if !ok {
		return
	}

	dir := filepath.Join("/data", "invoices")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("create invoice dir %s: %v", dir, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	path := filepath.Join(dir, number+".pdf")
	if err := os.WriteFile(path, body, 0o644); err != nil { // WriteFile truncates: overwrite on re-save
		log.Printf("write invoice %d pdf to %s: %v", id, path, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"saved","path":%q,"bytes":%d}`+"\n", path, len(body))
}

// SendEmail handles POST /invoices/{id}/email — renders the invoice PDF and
// emails it to the client via the configured Sender, marking the invoice sent
// only after a successful send.
func (h *InvoicesHandler) SendEmail(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}

	inv, err := store.GetInvoiceWithLines(h.DB, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeEmailAlert(w, "error", "invoice not found")
		} else {
			log.Printf("get invoice %d for email: %v", id, err)
			writeEmailAlert(w, "error", "internal error")
		}
		return
	}

	company, _, err := store.GetCompany(h.DB)
	if err != nil {
		log.Printf("get company for invoice email: %v", err)
		writeEmailAlert(w, "error", "internal error")
		return
	}

	if inv.Client.Email == "" {
		writeEmailAlert(w, "error", "client has no email address")
		return
	}

	if h.Cfg.ResendAPIKey == "" {
		writeEmailAlert(w, "error", "email not configured \u2014 set GINVOICE_RESEND_API_KEY")
		return
	}

	raw, err := pdf.RenderInvoice(inv, company)
	if err != nil {
		log.Printf("render invoice %d pdf: %v", id, err)
		writeEmailAlert(w, "error", "failed to render PDF")
		return
	}

	const maxAttachmentBytes = 40 * 1024 * 1024 // Resend limit is 40MB after base64 encoding
	encodedLen := base64.StdEncoding.EncodedLen(len(raw))
	if encodedLen > maxAttachmentBytes {
		writeEmailAlert(w, "error", "attachment too large to send")
		return
	}

	// Determine email template: client override > company default > built-in fallback
	subjectTmpl := inv.Client.EmailSubject
	bodyTmpl := inv.Client.EmailBody
	if subjectTmpl == "" {
		subjectTmpl = company.DefaultEmailSubject
		if subjectTmpl == "" {
			subjectTmpl = email.DefaultSubject
		}
	}
	if bodyTmpl == "" {
		bodyTmpl = company.DefaultEmailBody
		if bodyTmpl == "" {
			bodyTmpl = email.DefaultBody
		}
	}

	tmplData := email.TemplateData{
		CompanyName:    company.Name,
		CompanyWebsite: company.Website,
		CompanyPhone:   company.Phone,
		OwnerFirstName: company.OwnerFirstName,
		OwnerLastName:  company.OwnerLastName,
		InvoiceNumber:  inv.Number,
		ClientName:     inv.Client.Name,
		InvoiceTotal:   fmt.Sprintf("%.2f", float64(inv.Total)/100),
		InvoiceDueDate: inv.DueDate,
	}
	subject := email.RenderTemplate(subjectTmpl, tmplData)
	htmlBody := email.RenderTemplate(bodyTmpl, tmplData)
	if err := h.Sender.Send(r.Context(), inv.Client.Email, subject, htmlBody, inv.Number+".pdf", raw); err != nil {
		log.Printf("send invoice %d email to %s: %v", id, inv.Client.Email, err)
		writeEmailAlert(w, "error", "email send failed: "+err.Error())
		return
	}

	if _, err := h.DB.Exec(
		`UPDATE invoices SET status='sent', sent_at=datetime('now'), updated_at=datetime('now') WHERE id=?`,
		id); err != nil {
		log.Printf("mark invoice %d sent: %v", id, err)
		writeEmailAlert(w, "error", "email sent but failed to update invoice status")
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `<div class="alert alert-success">Email sent to %s</div>`, html.EscapeString(inv.Client.Email))
	fmt.Fprint(w, `<span id="status-badge" class="badge badge-sent" hx-swap-oob="outerHTML">sent</span>`)
	fmt.Fprint(w, `<span id="email-btn" hx-swap-oob="outerHTML"></span>`)
}

func writeEmailAlert(w http.ResponseWriter, class, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `<div class="alert alert-%s">%s</div>`, class, html.EscapeString(msg))
}
