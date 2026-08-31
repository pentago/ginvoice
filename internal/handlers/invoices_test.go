package handlers

import (
	"database/sql"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ginvoice/internal/config"
	"ginvoice/internal/email"
	"ginvoice/internal/store"
)

// newInvoicesTestServer spins up a real HTTP server (httptest) backed by a
// fresh temp-file SQLite DB with migrations applied, and registers the
// invoices routes exactly like cmd/app/main.go does.
func newInvoicesTestServer(t *testing.T) (*httptest.Server, *sql.DB) {
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
	ih := &InvoicesHandler{DB: db}
	mux.HandleFunc("GET /invoices", ih.List)
	mux.HandleFunc("GET /invoices/new", ih.New)
	mux.HandleFunc("GET /invoices/line-item", ih.LineItem)
	mux.HandleFunc("POST /invoices", ih.Create)
	mux.HandleFunc("GET /invoices/{id}", ih.View)
	mux.HandleFunc("GET /invoices/{id}/edit", ih.Edit)
	mux.HandleFunc("POST /invoices/{id}", ih.Update)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, db
}

// seedCompany inserts a default company row for tests that need it.
func seedCompany(t *testing.T, db *sql.DB) {
	t.Helper()
	company := store.Company{
		Name:              "Test Company",
		Address:           "123 Test St",
		Email:             "test@company.com",
		Phone:             "+1234567890",
		DefaultTaxRateBPS: 1000,
	}
	if err := store.UpsertCompany(db, company); err != nil {
		t.Fatalf("seed company: %v", err)
	}
}

// seedClient inserts a client for tests.
func seedClient(t *testing.T, db *sql.DB, name, email string) int64 {
	t.Helper()
	client := store.Client{
		Name:  name,
		Email: email,
	}
	id, err := store.CreateClient(db, client)
	if err != nil {
		t.Fatalf("seed client: %v", err)
	}
	return id
}

// seedService inserts a service for tests.
func seedService(t *testing.T, db *sql.DB, name, description string, price int64) int64 {
	t.Helper()
	svc := store.Service{
		Name:             name,
		Description:      description,
		DefaultUnitPrice: price,
		Unit:             "item",
	}
	id, err := store.CreateService(db, svc)
	if err != nil {
		t.Fatalf("seed service: %v", err)
	}
	return id
}

// S1: Create invoice with 2 lines, verify totals and number, then view it.
func TestInvoice_CreateAndView(t *testing.T) {
	srv, db := newInvoicesTestServer(t)
	seedCompany(t, db)
	clientID := seedClient(t, db, "Acme Corp", "acme@example.com")
	svcID1 := seedService(t, db, "Web Development", "Web development services", 1000)
	svcID2 := seedService(t, db, "Consulting", "Consulting services", 2000)

	data := url.Values{
		"client_id":          {fmt.Sprintf("%d", clientID)},
		"issue_date":         {"2026-01-15"},
		"due_date":           {"2026-02-15"},
		"tax_rate_pct":       {"10"},
		"notes":              {"Test invoice"},
		"line_service_id[0]": {fmt.Sprintf("%d", svcID1)},
		"line_qty[0]":        {"2"},
		"line_service_id[1]": {fmt.Sprintf("%d", svcID2)},
		"line_qty[1]":        {"1"},
	}

	resp := postForm(t, srv.URL+"/invoices", data)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create invoice: status = %d, want %d", resp.StatusCode, http.StatusSeeOther)
	}

	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/invoices/") {
		t.Fatalf("create invoice: Location = %q, want /invoices/{id}", loc)
	}

	// Extract invoice ID from location
	var invoiceID int64
	fmt.Sscanf(loc, "/invoices/%d", &invoiceID)

	// Verify invoice was created with correct totals
	inv, err := store.GetInvoiceWithLines(db, invoiceID)
	if err != nil {
		t.Fatalf("get invoice: %v", err)
	}

	// Expected: 2 * 1000 + 1 * 2000 = 4000 cents subtotal
	// Tax: 4000 * 1000 / 10000 = 400 cents
	// Total: 4400 cents
	if inv.Subtotal != 4000 {
		t.Errorf("subtotal = %d, want 4000", inv.Subtotal)
	}
	if inv.TaxAmount != 400 {
		t.Errorf("tax_amount = %d, want 400", inv.TaxAmount)
	}
	if inv.Total != 4400 {
		t.Errorf("total = %d, want 4400", inv.Total)
	}
	if inv.Number != "INV-2026-001" {
		t.Errorf("number = %q, want INV-2026-001", inv.Number)
	}
	if inv.ClientID != clientID {
		t.Errorf("client_id = %d, want %d", inv.ClientID, clientID)
	}
	if len(inv.Lines) != 2 {
		t.Errorf("lines count = %d, want 2", len(inv.Lines))
	}

	// GET view page
	status, body := getBody(t, srv.URL+loc)
	if status != http.StatusOK {
		t.Fatalf("GET %s: status = %d, want %d", loc, status, http.StatusOK)
	}
	if !strings.Contains(body, "INV-2026-001") {
		t.Errorf("view page does not contain invoice number")
	}
}

// S2: Sequential numbering across two invoices.
func TestInvoice_SequentialNumbering(t *testing.T) {
	srv, db := newInvoicesTestServer(t)
	seedCompany(t, db)
	clientID := seedClient(t, db, "Client A", "a@example.com")
	svcID := seedService(t, db, "Service", "General service", 10000)

	// Create first invoice
	data1 := url.Values{
		"client_id":          {fmt.Sprintf("%d", clientID)},
		"issue_date":         {"2026-01-01"},
		"tax_rate_pct":       {"0"},
		"line_service_id[0]": {fmt.Sprintf("%d", svcID)},
		"line_qty[0]":        {"1"},
	}
	resp1 := postForm(t, srv.URL+"/invoices", data1)
	if resp1.StatusCode != http.StatusSeeOther {
		t.Fatalf("create invoice 1: status = %d", resp1.StatusCode)
	}

	// Create second invoice
	data2 := url.Values{
		"client_id":          {fmt.Sprintf("%d", clientID)},
		"issue_date":         {"2026-01-02"},
		"tax_rate_pct":       {"0"},
		"line_service_id[0]": {fmt.Sprintf("%d", svcID)},
		"line_qty[0]":        {"1"},
	}
	resp2 := postForm(t, srv.URL+"/invoices", data2)
	if resp2.StatusCode != http.StatusSeeOther {
		t.Fatalf("create invoice 2: status = %d", resp2.StatusCode)
	}

	// Verify sequential numbers
	inv1, err := store.GetInvoice(db, 1)
	if err != nil {
		t.Fatalf("get invoice 1: %v", err)
	}
	inv2, err := store.GetInvoice(db, 2)
	if err != nil {
		t.Fatalf("get invoice 2: %v", err)
	}

	if inv1.Number != "INV-2026-001" {
		t.Errorf("invoice 1 number = %q, want INV-2026-001", inv1.Number)
	}
	if inv2.Number != "INV-2026-002" {
		t.Errorf("invoice 2 number = %q, want INV-2026-002", inv2.Number)
	}
}

// S3: Server-authoritative totals — tampered total is ignored.
func TestInvoice_ServerAuthoritativeTotals(t *testing.T) {
	srv, db := newInvoicesTestServer(t)
	seedCompany(t, db)
	clientID := seedClient(t, db, "Client B", "b@example.com")
	svcID1 := seedService(t, db, "Web Development", "Web development services", 1000)
	svcID2 := seedService(t, db, "Consulting", "Consulting services", 2000)

	data := url.Values{
		"client_id":          {fmt.Sprintf("%d", clientID)},
		"issue_date":         {"2026-01-01"},
		"tax_rate_pct":       {"10"},
		"line_service_id[0]": {fmt.Sprintf("%d", svcID1)},
		"line_qty[0]":        {"2"},
		"line_service_id[1]": {fmt.Sprintf("%d", svcID2)},
		"line_qty[1]":        {"1"},
		"total":              {"1"}, // tampered
	}

	resp := postForm(t, srv.URL+"/invoices", data)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create invoice: status = %d", resp.StatusCode)
	}

	var invoiceID int64
	fmt.Sscanf(resp.Header.Get("Location"), "/invoices/%d", &invoiceID)

	inv, err := store.GetInvoice(db, invoiceID)
	if err != nil {
		t.Fatalf("get invoice: %v", err)
	}

	// Server recomputes totals, ignoring client-submitted total
	if inv.Total != 4400 {
		t.Errorf("total = %d, want 4400 (server should ignore tampered total)", inv.Total)
	}
}

// S4: No lines rejects with 400.
func TestInvoice_NoLinesRejects400(t *testing.T) {
	srv, db := newInvoicesTestServer(t)
	seedCompany(t, db)
	clientID := seedClient(t, db, "Client C", "c@example.com")

	data := url.Values{
		"client_id":  {fmt.Sprintf("%d", clientID)},
		"issue_date": {"2026-01-01"},
		"tax_rate_pct": {"10"},
	}

	resp := postForm(t, srv.URL+"/invoices", data)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("create invoice without lines: status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}

	// Verify no invoice was created
	invoices, err := store.ListInvoices(db)
	if err != nil {
		t.Fatalf("list invoices: %v", err)
	}
	if len(invoices) != 0 {
		t.Errorf("invoices created despite missing lines: %d", len(invoices))
	}
}

// S5: No company row rejects with 400.
func TestInvoice_NoCompanyRejects400(t *testing.T) {
	srv, db := newInvoicesTestServer(t)
	clientID := seedClient(t, db, "Client D", "d@example.com")
	svcID := seedService(t, db, "Service", "General service", 10000)

	data := url.Values{
		"client_id":  {fmt.Sprintf("%d", clientID)},
		"issue_date": {"2026-01-01"},
		"tax_rate_pct": {"10"},
		"line_service_id[0]": {fmt.Sprintf("%d", svcID)},
		"line_qty[0]":        {"1"},
	}

	resp := postForm(t, srv.URL+"/invoices", data)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("create invoice without company: status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

// S6: Edit draft — update lines and totals.
func TestInvoice_EditDraft(t *testing.T) {
	srv, db := newInvoicesTestServer(t)
	seedCompany(t, db)
	clientID := seedClient(t, db, "Client E", "e@example.com")
	svcID := seedService(t, db, "Service", "General service", 1000)

	// Create invoice
	createData := url.Values{
		"client_id":          {fmt.Sprintf("%d", clientID)},
		"issue_date":         {"2026-01-01"},
		"tax_rate_pct":       {"0"},
		"line_service_id[0]": {fmt.Sprintf("%d", svcID)},
		"line_qty[0]":        {"1"},
	}
	resp := postForm(t, srv.URL+"/invoices", createData)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create invoice: status = %d", resp.StatusCode)
	}

	var invoiceID int64
	fmt.Sscanf(resp.Header.Get("Location"), "/invoices/%d", &invoiceID)

	// Verify initial total
	inv, err := store.GetInvoice(db, invoiceID)
	if err != nil {
		t.Fatalf("get invoice: %v", err)
	}
	if inv.Total != 1000 {
		t.Fatalf("initial total = %d, want 1000", inv.Total)
	}

	// Update invoice
	updateData := url.Values{
		"client_id":          {fmt.Sprintf("%d", clientID)},
		"issue_date":         {"2026-01-01"},
		"tax_rate_pct":       {"10"},
		"line_service_id[0]": {fmt.Sprintf("%d", svcID)},
		"line_qty[0]":        {"2"},
	}
	updateResp := postForm(t, srv.URL+fmt.Sprintf("/invoices/%d", invoiceID), updateData)
	if updateResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("update invoice: status = %d", updateResp.StatusCode)
	}

	// Verify updated totals
	updatedInv, err := store.GetInvoice(db, invoiceID)
	if err != nil {
		t.Fatalf("get updated invoice: %v", err)
	}
	// 2 * 1000 = 2000 cents, tax = 2000 * 1000 / 10000 = 200, total = 2200
	if updatedInv.Subtotal != 2000 {
		t.Errorf("subtotal = %d, want 2000", updatedInv.Subtotal)
	}
	if updatedInv.TaxAmount != 200 {
		t.Errorf("tax_amount = %d, want 200", updatedInv.TaxAmount)
	}
	if updatedInv.Total != 2200 {
		t.Errorf("total = %d, want 2200", updatedInv.Total)
	}

	// GET view shows new total
	_, viewBody := getBody(t, srv.URL+fmt.Sprintf("/invoices/%d", invoiceID))
	if !strings.Contains(viewBody, "22.00") {
		t.Errorf("view page does not show updated total")
	}
}

// S7: Sent invoice is immutable — 409 on edit and update.
func TestInvoice_SentImmutable(t *testing.T) {
	srv, db := newInvoicesTestServer(t)
	seedCompany(t, db)
	clientID := seedClient(t, db, "Client F", "f@example.com")
	svcID := seedService(t, db, "Service", "General service", 10000)

	// Create invoice
	createData := url.Values{
		"client_id":          {fmt.Sprintf("%d", clientID)},
		"issue_date":         {"2026-01-01"},
		"tax_rate_pct":       {"0"},
		"line_service_id[0]": {fmt.Sprintf("%d", svcID)},
		"line_qty[0]":        {"1"},
	}
	resp := postForm(t, srv.URL+"/invoices", createData)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create invoice: status = %d", resp.StatusCode)
	}

	var invoiceID int64
	fmt.Sscanf(resp.Header.Get("Location"), "/invoices/%d", &invoiceID)

	// Set status to 'sent' directly in DB
	if _, err := db.Exec("UPDATE invoices SET status = 'sent' WHERE id = ?", invoiceID); err != nil {
		t.Fatalf("set invoice status to sent: %v", err)
	}

	// Try to edit — should get 409
	editResp, err := http.Get(srv.URL + fmt.Sprintf("/invoices/%d/edit", invoiceID))
	if err != nil {
		t.Fatalf("GET edit page: %v", err)
	}
	editResp.Body.Close()
	if editResp.StatusCode != http.StatusConflict {
		t.Errorf("GET edit sent invoice: status = %d, want %d", editResp.StatusCode, http.StatusConflict)
	}

	// Try to update — should get 409
	updateData := url.Values{
		"client_id":          {fmt.Sprintf("%d", clientID)},
		"issue_date":         {"2026-01-01"},
		"tax_rate_pct":       {"0"},
		"line_service_id[0]": {fmt.Sprintf("%d", svcID)},
		"line_qty[0]":        {"1"},
	}
	updateResp := postForm(t, srv.URL+fmt.Sprintf("/invoices/%d", invoiceID), updateData)
	if updateResp.StatusCode != http.StatusConflict {
		t.Errorf("POST update sent invoice: status = %d, want %d", updateResp.StatusCode, http.StatusConflict)
	}
}

// S8: Client prefill — GET /invoices/new?client_id=X returns HTML with client name.
func TestInvoice_ClientPrefill(t *testing.T) {
	srv, db := newInvoicesTestServer(t)
	seedCompany(t, db)
	clientID := seedClient(t, db, "Prefill Client", "prefill@example.com")

	status, body := getBody(t, srv.URL+fmt.Sprintf("/invoices/new?client_id=%d", clientID))
	if status != http.StatusOK {
		t.Fatalf("GET /invoices/new?client_id=%d: status = %d", clientID, status)
	}
	if !strings.Contains(body, "Prefill Client") {
		t.Errorf("new invoice page does not contain client name")
	}
	if !strings.Contains(body, "prefill@example.com") {
		t.Errorf("new invoice page does not contain client email")
	}
}

// ListInvoicesPage returns HTML containing invoice numbers.
func TestInvoice_ListPage(t *testing.T) {
	srv, db := newInvoicesTestServer(t)
	seedCompany(t, db)
	clientID := seedClient(t, db, "List Client", "list@example.com")
	svcID := seedService(t, db, "Service", "General service", 10000)

	// Create an invoice
	createData := url.Values{
		"client_id":          {fmt.Sprintf("%d", clientID)},
		"issue_date":         {"2026-01-01"},
		"tax_rate_pct":       {"0"},
		"line_service_id[0]": {fmt.Sprintf("%d", svcID)},
		"line_qty[0]":        {"1"},
	}
	resp := postForm(t, srv.URL+"/invoices", createData)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create invoice: status = %d", resp.StatusCode)
	}

	// List invoices
	status, body := getBody(t, srv.URL+"/invoices")
	if status != http.StatusOK {
		t.Fatalf("GET /invoices: status = %d", status)
	}
	if !strings.Contains(body, "INV-2026-001") {
		t.Errorf("list page does not contain invoice number")
	}
}

// --- Email send (T9) ---

// newEmailTestServer spins up an HTTP server backed by a fresh temp-file SQLite DB
// with migrations applied, wiring InvoicesHandler with the given Resend API key and
// a FakeSender. Returns the server, DB, and fake for assertions.
func newEmailTestServer(t *testing.T, resendAPIKey string) (*httptest.Server, *sql.DB, *email.FakeSender) {
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

	fake := &email.FakeSender{}
	mux := http.NewServeMux()
	ih := &InvoicesHandler{DB: db, Cfg: &config.Config{ResendAPIKey: resendAPIKey}, Sender: fake}
	mux.HandleFunc("POST /invoices/{id}/email", ih.SendEmail)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, db, fake
}

// seedDraftInvoice inserts a company, a client with the given email, and a draft
// invoice with one line; returns the invoice id.
func seedDraftInvoice(t *testing.T, db *sql.DB, clientEmail string) int64 {
	t.Helper()
	seedCompany(t, db)
	clientID := seedClient(t, db, "Email Client", clientEmail)
	id, err := store.CreateInvoice(db, store.Invoice{
		Number:    "INV-2026-001",
		ClientID:  clientID,
		IssueDate: "2026-01-15",
		DueDate:   "2026-02-15",
		Status:    "draft",
		Currency:  "EUR",
		Subtotal:  1000,
		TaxRate:   0,
		TaxAmount: 0,
		Total:     1000,
	}, []store.InvoiceLine{{
		Description: "Consulting",
		Quantity:    1,
		UnitPrice:   1000,
		LineTotal:   1000,
	}})
	if err != nil {
		t.Fatalf("seed draft invoice: %v", err)
	}
	return id
}

// S2: no Resend API key configured → 503 and no send attempt.
func TestEmail_ErrorWhenKeyUnset(t *testing.T) {
	srv, db, fake := newEmailTestServer(t, "")
	id := seedDraftInvoice(t, db, "client@example.com")

	resp := postForm(t, srv.URL+fmt.Sprintf("/invoices/%d/email", id), url.Values{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("email without API key: status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "alert-error") || !strings.Contains(string(body), "email not configured") {
		t.Errorf("body = %q, want alert-error with 'email not configured'", string(body))
	}
	if len(fake.Calls) != 0 {
		t.Errorf("sender called %d times, want 0", len(fake.Calls))
	}
}

// S3: client has no email address → 400 and no send attempt.
func TestEmail_ErrorWhenNoClientEmail(t *testing.T) {
	srv, db, fake := newEmailTestServer(t, "re_test_key")
	id := seedDraftInvoice(t, db, "")

	resp := postForm(t, srv.URL+fmt.Sprintf("/invoices/%d/email", id), url.Values{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("email for client without address: status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "alert-error") || !strings.Contains(string(body), "client has no email address") {
		t.Errorf("body = %q, want alert-error with 'client has no email address'", string(body))
	}
	if len(fake.Calls) != 0 {
		t.Errorf("sender called %d times, want 0", len(fake.Calls))
	}
}

// S1 happy path: send succeeds → 200, FakeSender got PDF attachment, invoice marked sent.
func TestEmail_SendsAndMarksSent(t *testing.T) {
	srv, db, fake := newEmailTestServer(t, "re_test_key")
	id := seedDraftInvoice(t, db, "client@example.com")

	resp := postForm(t, srv.URL+fmt.Sprintf("/invoices/%d/email", id), url.Values{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("email send: status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "alert-success") || !strings.Contains(string(body), "Email sent to") {
		t.Errorf("body = %q, want alert-success with 'Email sent to'", string(body))
	}

	if len(fake.Calls) != 1 {
		t.Fatalf("sender calls = %d, want 1", len(fake.Calls))
	}
	call := fake.Calls[0]
	if call.To != "client@example.com" {
		t.Errorf("to = %q, want client@example.com", call.To)
	}
	if call.Subject != "Test Company Invoice INV-2026-001" {
		t.Errorf("subject = %q, want %q", call.Subject, "Test Company Invoice INV-2026-001")
	}
	if call.AttachmentName != "INV-2026-001.pdf" {
		t.Errorf("attachment name = %q, want INV-2026-001.pdf", call.AttachmentName)
	}
	if call.AttachmentLen == 0 {
		t.Errorf("attachment is empty, want non-empty PDF bytes")
	}
	if !strings.Contains(call.HTML, "INV-2026-001") {
		t.Errorf("html body does not mention invoice number: %q", call.HTML)
	}

	inv, err := store.GetInvoice(db, id)
	if err != nil {
		t.Fatalf("get invoice after send: %v", err)
	}
	if inv.Status != "sent" {
		t.Errorf("status = %q, want sent", inv.Status)
	}
	if inv.SentAt == "" {
		t.Errorf("sent_at not set after successful send")
	}
}

// S4: sender failure → 502 and invoice stays draft.
func TestEmail_ErrorOnSendFailure(t *testing.T) {
	srv, db, fake := newEmailTestServer(t, "re_test_key")
	id := seedDraftInvoice(t, db, "client@example.com")
	fake.Err = fmt.Errorf("resend HTTP 500")

	resp := postForm(t, srv.URL+fmt.Sprintf("/invoices/%d/email", id), url.Values{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("email send failure: status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "alert-error") || !strings.Contains(string(body), "email send failed") {
		t.Errorf("body = %q, want alert-error with 'email send failed'", string(body))
	}

	inv, err := store.GetInvoice(db, id)
	if err != nil {
		t.Fatalf("get invoice after failed send: %v", err)
	}
	if inv.Status != "draft" {
		t.Errorf("status = %q, want draft (must not mark sent on failure)", inv.Status)
	}
	if inv.SentAt != "" {
		t.Errorf("sent_at = %q, want empty on failed send", inv.SentAt)
	}
}
