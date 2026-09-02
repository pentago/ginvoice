package pdf

import (
	"bytes"
	_ "embed"
	"fmt"
	"html"
	"log"
	"os"
	"strings"
	"text/template"

	goweasyprint "github.com/benoitkugler/go-weasyprint"
	"github.com/benoitkugler/webrender/text"

	"ginvoice/internal/email"
	"ginvoice/internal/store"
)

// defaultInvoiceTemplate is the built-in invoice layout: plain HTML/CSS
// rendered by go-weasyprint (pure Go, no browser). Users can override it by
// placing an HTML file at templateOverridePath.
//
//go:embed invoice.html
var defaultInvoiceTemplate string

const templateOverridePath = "/data/invoice_template.html"

// itemRow is one invoice line, all values preformatted.
type itemRow struct {
	Description string
	Currency    string
	UnitPrice   string
	Quantity    string
	Total       string
}

// invoiceView is the display model for invoice.html. All user-controlled
// strings are HTML-escaped here; multi-line blocks are joined with <br>.
type invoiceView struct {
	MarginMM    float64
	HeadingSize float64
	BodySize    float64
	LabelSize   float64
	GrandSize   float64

	AccentColor      string
	TextColor        string
	MutedColor       string
	DividerColor     string
	TableHeaderBg    string
	TableHeaderColor string

	Number    string
	IssueDate string
	DueDate   string
	LogoData  string

	ClientLines  string
	CompanyLines string
	ItemRows     []itemRow

	SubtotalStr   string
	DiscountLabel string
	DiscountStr   string
	TaxLabel      string
	TaxStr        string
	TotalStr      string

	InvoiceNotesHTML string
	NotesHTML        string
}

func formatMoney(cents int64, currency string) string {
	amount := fmt.Sprintf("%.2f", float64(cents)/100)
	if currency == "" {
		return amount
	}
	return currency + " " + amount
}

func RenderInvoice(inv store.Invoice, company store.Company) ([]byte, error) {
	return RenderInvoiceWithConfig(inv, company, DefaultConfig())
}

func RenderInvoiceWithConfig(inv store.Invoice, company store.Company, cfg TemplateConfig) ([]byte, error) {
	fc, err := fontConfig()
	if err != nil {
		return nil, err
	}
	view := buildInvoiceView(inv, company, cfg)

	// User override wins, but a broken template must not break invoicing.
	if raw, err := os.ReadFile(templateOverridePath); err == nil && len(raw) > 0 {
		pdf, err := render(string(raw), view, fc)
		if err != nil {
			log.Printf("invalid %s, using built-in invoice template: %v", templateOverridePath, err)
		} else {
			return pdf, nil
		}
	}
	return render(defaultInvoiceTemplate, view, fc)
}

func render(tmplText string, view invoiceView, fc text.FontConfiguration) ([]byte, error) {
	tmpl, err := template.New("invoice").Parse(tmplText)
	if err != nil {
		return nil, fmt.Errorf("parse invoice template: %w", err)
	}
	var htmlBuf bytes.Buffer
	if err := tmpl.Execute(&htmlBuf, view); err != nil {
		return nil, fmt.Errorf("execute invoice template: %w", err)
	}
	var out bytes.Buffer
	if err := goweasyprint.HtmlToPdf(&out, goweasyprint.InputString(htmlBuf.String()), fc); err != nil {
		return nil, fmt.Errorf("render pdf: %w", err)
	}
	return out.Bytes(), nil
}

func buildInvoiceView(inv store.Invoice, company store.Company, cfg TemplateConfig) invoiceView {
	currency := inv.Currency
	if currency == "" {
		currency = "EUR"
	}

	clientName := inv.Client.CompanyName
	if clientName == "" {
		clientName = inv.Client.Name
	}

	rows := make([]itemRow, len(inv.Lines))
	for i, line := range inv.Lines {
		rows[i] = itemRow{
			Description: html.EscapeString(line.Description),
			Currency:    currency,
			UnitPrice:   formatMoney(line.UnitPrice, ""),
			Quantity:    fmt.Sprintf("%.2f", line.Quantity),
			Total:       formatMoney(line.LineTotal, ""),
		}
	}

	v := invoiceView{
		MarginMM:    cfg.MarginMM,
		HeadingSize: cfg.HeadingSize,
		BodySize:    cfg.BodySize,
		LabelSize:   cfg.LabelSize,
		GrandSize:   cfg.LabelSize + 2,

		AccentColor:      cfg.AccentColor,
		TextColor:        cfg.TextColor,
		MutedColor:       cfg.MutedColor,
		DividerColor:     cfg.DividerColor,
		TableHeaderBg:    cfg.TableHeaderBg,
		TableHeaderColor: cfg.TableHeaderColor,

		Number:    html.EscapeString(inv.Number),
		IssueDate: html.EscapeString(inv.IssueDate),
		DueDate:   html.EscapeString(inv.DueDate),
		LogoData:  company.LogoData,

		ClientLines: joinHTML([]string{clientName, inv.Client.Address, inv.Client.Email}),
		CompanyLines: joinHTML([]string{
			company.Name,
			company.AddressLine1,
			company.AddressLine2,
			strings.TrimSpace(company.PostalCode + " " + company.City),
			joinComma(company.State, company.Country),
			strIf(company.IBAN != "", "IBAN: "+company.IBAN),
			strIf(company.TaxID != "", "Tax ID: "+company.TaxID),
		}),
		ItemRows:    rows,
		SubtotalStr: formatMoney(inv.Subtotal, currency),
		TotalStr:    formatMoney(inv.Total, currency),

		InvoiceNotesHTML: toHTML(invoiceNotes(inv, company)),
	}

	if inv.DiscountBPS > 0 {
		v.DiscountLabel = fmt.Sprintf("Discount (%.2f%%)", float64(inv.DiscountBPS)/100)
		v.DiscountStr = "- " + formatMoney(inv.DiscountAmount, currency)
	}
	if inv.TaxRate > 0 {
		v.TaxLabel = fmt.Sprintf("Tax (%.2f%%)", float64(inv.TaxRate)/100)
		v.TaxStr = formatMoney(inv.TaxAmount, currency)
	}
	if cfg.ShowNotes {
		v.NotesHTML = toHTML(inv.Notes)
	}
	return v
}

// invoiceNotes resolves the notes fallback chain (client → company → built-in
// default) and renders template variables like {{companyCity}}.
func invoiceNotes(inv store.Invoice, company store.Company) string {
	notes := inv.Client.InvoiceNotes
	if notes == "" {
		notes = company.InvoiceNotes
	}
	if notes == "" {
		notes = store.DefaultInvoiceNotes
	}
	return email.RenderTemplate(notes, email.TemplateDataFor(inv, company))
}

// joinHTML HTML-escapes each line, drops blanks and joins with <br>.
func joinHTML(lines []string) string {
	var out []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			out = append(out, html.EscapeString(l))
		}
	}
	return strings.Join(out, "<br>")
}

// toHTML escapes the text and converts newlines to <br>. Empty stays empty.
func toHTML(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	return strings.ReplaceAll(html.EscapeString(s), "\n", "<br>")
}

func strIf(cond bool, s string) string {
	if cond {
		return s
	}
	return ""
}

func joinComma(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + ", " + b
}
