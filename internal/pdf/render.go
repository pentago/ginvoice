// Package pdf renders invoices as PDF documents using maroto v2 and
// optimizes them with pdfcpu.
package pdf

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/johnfercher/maroto/v2"
	"github.com/johnfercher/maroto/v2/pkg/components/col"
	"github.com/johnfercher/maroto/v2/pkg/components/image"
	"github.com/johnfercher/maroto/v2/pkg/components/row"
	"github.com/johnfercher/maroto/v2/pkg/components/text"
	"github.com/johnfercher/maroto/v2/pkg/config"
	"github.com/johnfercher/maroto/v2/pkg/consts/align"
	"github.com/johnfercher/maroto/v2/pkg/consts/fontstyle"
	"github.com/johnfercher/maroto/v2/pkg/core"
	"github.com/johnfercher/maroto/v2/pkg/props"
	"github.com/johnfercher/maroto/v2/pkg/consts/extension"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"

	"ginvoice/internal/store"
)

// formatMoney formats integer cents as an amount with exactly two decimal
// places, prefixed by the currency code when one is given. Cents are always
// divided by 100 before display — raw cents must never appear in a PDF.
func formatMoney(cents int64, currency string) string {
	amount := fmt.Sprintf("%.2f", float64(cents)/100)
	if currency == "" {
		return amount
	}
	return currency + " " + amount
}

// OptimizeBytes runs pdfcpu's optimize pass (font/image dedup + object
// streams) over in-memory PDF bytes.
func init() {
	// Disable pdfcpu's config file loading. It tries to create $HOME/.config/pdfcpu/
	// which fails in read-only containers. We only use the optimize pass, which
	// works fine with built-in defaults.
	model.ConfigPath = "disable"
}

func OptimizeBytes(input []byte) ([]byte, error) {
	r := bytes.NewReader(input)
	var buf bytes.Buffer
	if err := api.Optimize(r, &buf, nil); err != nil {
		return nil, fmt.Errorf("pdfcpu optimize: %w", err)
	}
	return buf.Bytes(), nil
}

// RenderInvoice generates a compressed PDF for the given invoice and company.
func RenderInvoice(inv store.Invoice, company store.Company) ([]byte, error) {
	cfg := config.NewBuilder().
		WithPageNumber(props.PageNumber{Pattern: "Page {current} of {total}"}).
		WithCompression(true).
		Build()

	m := maroto.New(cfg)

	addCompanyHeader(m, company)

	// Invoice title + dates.
	m.AddRows(row.New(4))
	m.AddRows(row.New(10).Add(
		col.New(6).Add(text.New("INVOICE "+inv.Number, props.Text{Size: 14, Style: fontstyle.Bold})),
		col.New(6).Add(text.New(dateLine("Issued", inv.IssueDate), props.Text{Align: align.Right})),
	))
	if inv.DueDate != "" {
		m.AddRows(row.New(6).Add(
			col.New(12).Add(text.New(dateLine("Due", inv.DueDate), props.Text{Align: align.Right})),
		))
	}

	// Client block.
	if inv.Client.Name != "" || inv.Client.CompanyName != "" {
		m.AddRows(row.New(4))
		m.AddRows(row.New(8).Add(
			col.New(12).Add(text.New("Bill To:", props.Text{Style: fontstyle.Bold})),
		))
		displayName := inv.Client.Name
		if inv.Client.CompanyName != "" {
			if displayName != "" {
				displayName += " (" + inv.Client.CompanyName + ")"
			} else {
				displayName = inv.Client.CompanyName
			}
		}
		m.AddRows(row.New().Add(col.New(12).Add(text.New(displayName))))
		for _, l := range []string{inv.Client.Address, inv.Client.Email} {
			if l != "" {
				m.AddRows(row.New().Add(col.New(12).Add(text.New(l))))
			}
		}
	}

	// Line items table.
	currency := inv.Currency
	if currency == "" {
		currency = "EUR"
	}
	m.AddRows(row.New(4))
	m.AddRows(row.New(8).Add(
		col.New(6).Add(text.New("Description", props.Text{Style: fontstyle.Bold})),
		col.New(2).Add(text.New("Qty", props.Text{Style: fontstyle.Bold, Align: align.Right})),
		col.New(2).Add(text.New("Unit Price", props.Text{Style: fontstyle.Bold, Align: align.Right})),
		col.New(2).Add(text.New("Total", props.Text{Style: fontstyle.Bold, Align: align.Right})),
	))
	for _, line := range inv.Lines {
		m.AddRows(row.New(6).Add(
			col.New(6).Add(text.New(line.Description)),
			col.New(2).Add(text.New(fmt.Sprintf("%.2f", line.Quantity), props.Text{Align: align.Right})),
			col.New(2).Add(text.New(formatMoney(line.UnitPrice, ""), props.Text{Align: align.Right})),
			col.New(2).Add(text.New(formatMoney(line.LineTotal, ""), props.Text{Align: align.Right})),
		))
	}

	// Totals.
	m.AddRows(row.New(4))
	m.AddRows(row.New(6).Add(
		col.New(10).Add(text.New("Subtotal", props.Text{Align: align.Right})),
		col.New(2).Add(text.New(formatMoney(inv.Subtotal, ""), props.Text{Align: align.Right})),
	))
	taxPct := float64(inv.TaxRate) / 100 // basis points -> percent
	m.AddRows(row.New(6).Add(
		col.New(10).Add(text.New(fmt.Sprintf("Tax (%.2f%%)", taxPct), props.Text{Align: align.Right})),
		col.New(2).Add(text.New(formatMoney(inv.TaxAmount, ""), props.Text{Align: align.Right})),
	))
	m.AddRows(row.New(8).Add(
		col.New(10).Add(text.New("TOTAL "+currency, props.Text{Style: fontstyle.Bold, Align: align.Right})),
		col.New(2).Add(text.New(formatMoney(inv.Total, ""), props.Text{Style: fontstyle.Bold, Align: align.Right})),
	))

	// Notes.
	if inv.Notes != "" {
		m.AddRows(row.New(4))
		m.AddRows(row.New().Add(
			col.New(12).Add(text.New("Notes: "+inv.Notes)),
		))
	}

	doc, err := m.Generate()
	if err != nil {
		return nil, fmt.Errorf("maroto generate: %w", err)
	}
	return doc.GetBytes(), nil
}

// addCompanyHeader renders the logo (from embedded base64 data) next to the
// company contact block.
func addCompanyHeader(m core.Maroto, company store.Company) {
	infoCols := func(size int) []core.Col {
		cols := []core.Col{
			col.New(size).Add(text.New(company.Name, props.Text{Size: 16, Style: fontstyle.Bold})),
		}
		details := []string{company.Address, company.Email}
		if company.TaxID != "" {
			details = append(details, "Tax ID: "+company.TaxID)
		}
		for _, d := range details {
			if d == "" {
				continue
			}
			cols = append(cols, col.New(size).Add(text.New(d)))
		}
		return cols
	}

	logoCol := 0
	var logoRow core.Row
	if company.LogoData != "" {
		if logoBytes, ext, ok := decodeDataURI(company.LogoData); ok {
			logoCol = 3
			logoRow = row.New(18).Add(
				col.New(logoCol).Add(image.NewFromBytes(logoBytes, ext)),
			)
		}
	}

	if logoRow != nil {
		m.AddRows(logoRow)
		m.AddRows(row.New().Add(infoCols(12-logoCol)...))
		return
	}
	m.AddRows(row.New().Add(infoCols(12)...))
}

// decodeDataURI extracts raw bytes and a maroto extension type from a
// data:image/...;base64,... URI. Returns ok=false if the URI is malformed
// or the MIME type is not a supported image format.
func decodeDataURI(dataURI string) ([]byte, extension.Type, bool) {
	// data:image/png;base64,<data>
	const prefix = "data:"
	if !strings.HasPrefix(dataURI, prefix) {
		return nil, "", false
	}
	semiIdx := strings.Index(dataURI, ";")
	base64Idx := strings.Index(dataURI, ";base64,")
	if semiIdx < 0 || base64Idx < 0 {
		return nil, "", false
	}
	mimeType := dataURI[len(prefix):semiIdx]
	b64Data := dataURI[base64Idx+len(";base64,"):]
	decoded, err := base64.StdEncoding.DecodeString(b64Data)
	if err != nil {
		return nil, "", false
	}
	var ext extension.Type
	switch mimeType {
	case "image/png":
		ext = extension.Png
	case "image/jpeg":
		ext = extension.Jpg
	default:
		return nil, "", false
	}
	return decoded, ext, true
}

// dateLine formats a labelled date, returning empty for blank input so the
// caller can skip rendering entirely.
func dateLine(label, date string) string {
	if date == "" {
		return ""
	}
	return label + ": " + date
}
