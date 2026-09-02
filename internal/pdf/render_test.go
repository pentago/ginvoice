package pdf_test

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"

	"ginvoice/internal/pdf"
	"ginvoice/internal/store"
)

// TestMain points the renderer's data dir (fonts extraction, logo file) at a
// temp dir so tests pass on dev machines without a writable /data.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ginvoice-pdf-test")
	if err != nil {
		panic(err)
	}
	os.Setenv("GINVOICE_DATA_DIR", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

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
		Name:         "My Company",
		AddressLine1: "456 Business Ave",
		City:         "Berlin",
		Country:      "Germany",
		Email:        "hello@mycompany.example",
		TaxID:        "DE123456789",
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

// testLogoDataURI returns a valid PNG logo as a data URI.
func testLogoDataURI(t *testing.T) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := range 64 {
		for x := range 64 {
			img.Set(x, y, color.RGBA{R: 200, G: 30, B: 30, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

// TestRenderInvoice_Logo verifies S3: a stored data-URI logo must be embedded
// in the PDF as an image.
func TestRenderInvoice_Logo(t *testing.T) {
	co := testCompany()
	co.LogoData = testLogoDataURI(t)

	b, err := pdf.RenderInvoice(testInvoice(), co)
	if err != nil {
		t.Fatalf("RenderInvoice with logo: %v", err)
	}
	if !bytes.Contains(b, []byte("/Image")) {
		t.Error("no /Image XObject in PDF — logo was dropped")
	}
}
