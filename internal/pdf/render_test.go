package pdf_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"

	"ginvoice/internal/pdf"
	"ginvoice/internal/store"
)

func testInvoice() store.Invoice {
	return store.Invoice{
		Number:    "INV-2026-001",
		IssueDate: "2026-01-01",
		DueDate:   "2026-01-31",
		Currency:  "EUR",
		TaxRate:   1000, // 10% in basis points
		Lines: []store.InvoiceLine{
			{Description: "Web Design", Quantity: 2, UnitPrice: 5000, LineTotal: 10000},
			{Description: "Hosting (months)", Quantity: 1.5, UnitPrice: 2000, LineTotal: 3000},
		},
		Subtotal:  13000,
		TaxAmount: 1300,
		Total:     14300,
		Client:    store.Client{Name: "Acme Corp", Address: "123 Main St", Email: "billing@acme.example"},
	}
}

func testCompany() store.Company {
	return store.Company{
		Name:    "My Company",
		Address: "456 Business Ave",
		Email:   "hello@mycompany.example",
		TaxID:   "DE123456789",
	}
}

// TestRenderInvoice_NonEmpty verifies S1: output is non-empty PDF magic bytes.
func TestRenderInvoice_NonEmpty(t *testing.T) {
	b, err := pdf.RenderInvoice(testInvoice(), testCompany())
	if err != nil {
		t.Fatalf("RenderInvoice: %v", err)
	}
	if len(b) == 0 {
		t.Fatal("got empty bytes")
	}
	if !bytes.HasPrefix(b, []byte("%PDF-")) {
		prefix := b
		if len(prefix) > 20 {
			prefix = prefix[:20]
		}
		t.Errorf("does not start with %%PDF-, starts with: %q", prefix)
	}
}

// TestRenderInvoice_PdfcpuValidates verifies S1: pdfcpu accepts the document.
func TestRenderInvoice_PdfcpuValidates(t *testing.T) {
	b, err := pdf.RenderInvoice(testInvoice(), testCompany())
	if err != nil {
		t.Fatalf("RenderInvoice: %v", err)
	}
	r := bytes.NewReader(b)
	if err := api.Validate(r, nil); err != nil {
		t.Errorf("pdfcpu validate: %v", err)
	}
}

// TestRenderInvoice_ZeroLines verifies S2: an invoice with no lines must not
// panic and must still produce a valid PDF.
func TestRenderInvoice_ZeroLines(t *testing.T) {
	inv := testInvoice()
	inv.Lines = nil
	inv.Subtotal = 0
	inv.TaxAmount = 0
	inv.Total = 0

	b, err := pdf.RenderInvoice(inv, testCompany())
	if err != nil {
		t.Fatalf("zero-line invoice panicked or errored: %v", err)
	}
	if len(b) == 0 {
		t.Fatal("zero-line invoice produced empty bytes")
	}
	if !bytes.HasPrefix(b, []byte("%PDF-")) {
		t.Error("zero-line invoice did not produce a PDF")
	}
}

// TestRenderInvoice_SavedSizeSmaller verifies S3: the optimized bytes can be
// written to disk and are no larger than the raw maroto output.
func TestRenderInvoice_SavedSizeSmaller(t *testing.T) {
	raw, err := pdf.RenderInvoice(testInvoice(), testCompany())
	if err != nil {
		t.Fatalf("RenderInvoice: %v", err)
	}

	opt, err := pdf.OptimizeBytes(raw)
	if err != nil {
		t.Fatalf("OptimizeBytes: %v", err)
	}
	if len(opt) == 0 {
		t.Fatal("optimized bytes are empty")
	}
	if len(opt) > len(raw) {
		t.Errorf("optimized size %d larger than raw %d", len(opt), len(raw))
	}
	if err := api.Validate(bytes.NewReader(opt), nil); err != nil {
		t.Errorf("pdfcpu validate optimized: %v", err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "invoice.pdf")
	if err := os.WriteFile(path, opt, 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if fi.Size() == 0 {
		t.Fatal("saved file is empty")
	}
}
