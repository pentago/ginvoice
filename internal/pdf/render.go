package pdf

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/gpdf-dev/gpdf/document"
	"github.com/gpdf-dev/gpdf/pdf"
	"github.com/gpdf-dev/gpdf/template"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"

	"ginvoice/internal/store"
)

const fontFamily = "goregular"

func formatMoney(cents int64, currency string) string {
	amount := fmt.Sprintf("%.2f", float64(cents)/100)
	if currency == "" {
		return amount
	}
	return currency + " " + amount
}

func hexColor(hex string) pdf.Color {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return pdf.Black
	}
	r, g, b := 0, 0, 0
	fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
	return pdf.RGB(float64(r)/255, float64(g)/255, float64(b)/255)
}

func RenderInvoice(inv store.Invoice, company store.Company) ([]byte, error) {
	return RenderInvoiceWithConfig(inv, company, DefaultConfig())
}

func RenderInvoiceWithConfig(inv store.Invoice, company store.Company, cfg TemplateConfig) ([]byte, error) {
	accent := hexColor(cfg.AccentColor)
	textCol := hexColor(cfg.TextColor)
	muted := hexColor(cfg.MutedColor)
	divider := hexColor(cfg.DividerColor)

	currency := inv.Currency
	if currency == "" {
		currency = "EUR"
	}

	doc := template.New(
		template.WithFont(fontFamily, goregular.TTF),
		template.WithFont(fontFamily+"-Bold", gobold.TTF),
		template.WithDefaultFont(fontFamily, cfg.BodySize),
		template.WithPageSize(document.A4),
		template.WithMargins(document.UniformEdges(document.Mm(cfg.MarginMM))),
		template.WithMetadata(document.DocumentMetadata{
			Title:  "Invoice " + inv.Number,
			Author: company.Name,
		}),
	)

	page := doc.AddPage()

	// ── 1. Title + logo ──
	page.AutoRow(func(r *template.RowBuilder) {
		r.Col(8, func(c *template.ColBuilder) {
			c.Text("INVOICE", template.FontSize(cfg.HeadingSize), template.Bold(), template.TextColor(accent))
		})
		r.Col(4, func(c *template.ColBuilder) {
			if company.LogoData != "" {
				if logoBytes, ok := decodeDataURI(company.LogoData); ok {
					c.Image(logoBytes, template.FitHeight(document.Mm(20)))
				}
			}
		})
	})

	page.AutoRow(func(r *template.RowBuilder) {
		r.Col(12, func(c *template.ColBuilder) { c.Spacer(document.Mm(16)) })
	})

	// ── 2. Three-column header: issued to | invoice info | from ──
	labelStyle := []template.TextOption{
		template.FontSize(cfg.LabelSize), template.Bold(), template.TextColor(muted),
	}
	bodyStyle := []template.TextOption{
		template.FontSize(cfg.BodySize), template.TextColor(textCol),
	}

	clientName := inv.Client.CompanyName
	if clientName == "" {
		clientName = inv.Client.Name
	}
	clientLines := filterEmpty([]string{clientName, inv.Client.Address, inv.Client.Email})

	companyLines := filterEmpty([]string{company.Name, company.Address})
	bankingLines := filterEmpty([]string{
		strIf(company.IBAN != "", "IBAN: "+company.IBAN),
		strIf(company.TaxID != "", "Tax ID: "+company.TaxID),
	})

	page.AutoRow(func(r *template.RowBuilder) {
		r.Col(4, func(c *template.ColBuilder) {
			c.Text("ISSUED TO:", labelStyle...)
			for _, line := range clientLines {
				c.Text(line, bodyStyle...)
			}
		})
		r.Col(4, func(c *template.ColBuilder) {
			c.Text("INVOICE NO:", labelStyle...)
			c.Text(inv.Number, bodyStyle...)
			c.Text("DATE:", labelStyle...)
			c.Text(inv.IssueDate, bodyStyle...)
			if inv.DueDate != "" {
				c.Text("DUE DATE:", labelStyle...)
				c.Text(inv.DueDate, bodyStyle...)
			}
		})
		r.Col(4, func(c *template.ColBuilder) {
			c.Text("FROM:", labelStyle...)
			for _, line := range companyLines {
				c.Text(line, bodyStyle...)
			}
			for _, line := range bankingLines {
				c.Text(line, bodyStyle...)
			}
		})
	})

	page.AutoRow(func(r *template.RowBuilder) {
		r.Col(12, func(c *template.ColBuilder) { c.Spacer(document.Mm(16)) })
	})

	// ── 3. Table ──
	header := []string{"SERVICE", "CURRENCY", "RATE", "QTY", "TOTAL"}

	rows := make([][]string, len(inv.Lines))
	for i, line := range inv.Lines {
		rows[i] = []string{
			line.Description,
			currency,
			formatMoney(line.UnitPrice, ""),
			fmt.Sprintf("%.2f", line.Quantity),
			formatMoney(line.LineTotal, ""),
		}
	}

	theaderOpts := []template.TextOption{template.TextColor(textCol), template.Bold()}
	if cfg.TableHeaderBg != "" {
		theaderOpts = append(theaderOpts, template.BgColor(hexColor(cfg.TableHeaderBg)))
	}

	page.AutoRow(func(r *template.RowBuilder) {
		r.Col(12, func(c *template.ColBuilder) {
			c.Table(header, rows,
				template.ColumnWidths(45, 15, 15, 10, 15),
				template.TableHeaderStyle(theaderOpts...),
			)
		})
	})

	page.AutoRow(func(r *template.RowBuilder) {
		r.Col(12, func(c *template.ColBuilder) {
			c.Spacer(document.Mm(5))
			c.Line(template.LineColor(divider), template.LineThickness(document.Pt(0.5)))
			c.Spacer(document.Mm(5))
		})
	})

	// ── 4. Totals ──
	totLabel := []template.TextOption{
		template.FontSize(cfg.BodySize), template.Bold(), template.TextColor(accent), template.AlignRight(),
	}
	totVal := []template.TextOption{
		template.FontSize(cfg.BodySize), template.Bold(), template.TextColor(accent), template.AlignRight(),
	}
	taxStyle := []template.TextOption{
		template.FontSize(cfg.BodySize), template.TextColor(textCol), template.AlignRight(),
	}
	bigLabel := []template.TextOption{
		template.FontSize(cfg.LabelSize + 2), template.Bold(), template.TextColor(accent), template.AlignRight(),
	}
	bigVal := []template.TextOption{
		template.FontSize(cfg.LabelSize + 2), template.Bold(), template.TextColor(accent), template.AlignRight(),
	}

	page.AutoRow(func(r *template.RowBuilder) {
		r.Col(9, func(c *template.ColBuilder) { c.Text("SUBTOTAL", totLabel...) })
		r.Col(3, func(c *template.ColBuilder) { c.Text(formatMoney(inv.Subtotal, currency), totVal...) })
	})

	if inv.DiscountBPS > 0 {
		discountPct := float64(inv.DiscountBPS) / 100
		page.AutoRow(func(r *template.RowBuilder) {
			r.Col(9, func(c *template.ColBuilder) {
				c.Text(fmt.Sprintf("DISCOUNT (%.2f%%)", discountPct), totLabel...)
			})
			r.Col(3, func(c *template.ColBuilder) {
				c.Text("- "+formatMoney(inv.DiscountAmount, currency), totVal...)
			})
		})
	}

	if inv.TaxRate > 0 {
		taxPct := float64(inv.TaxRate) / 100
		page.AutoRow(func(r *template.RowBuilder) {
			r.Col(9, func(c *template.ColBuilder) {
				c.Text(fmt.Sprintf("Tax (%.2f%%)", taxPct), taxStyle...)
			})
			r.Col(3, func(c *template.ColBuilder) {
				c.Text(formatMoney(inv.TaxAmount, currency), taxStyle...)
			})
		})
	}

	page.AutoRow(func(r *template.RowBuilder) {
		r.Col(9, func(c *template.ColBuilder) { c.Text("TOTAL", bigLabel...) })
		r.Col(3, func(c *template.ColBuilder) { c.Text(formatMoney(inv.Total, currency), bigVal...) })
	})

	invoiceNotes := inv.Client.InvoiceNotes
	if invoiceNotes == "" {
		invoiceNotes = company.InvoiceNotes
	}
	if invoiceNotes != "" {
		page.AutoRow(func(r *template.RowBuilder) {
			r.Col(12, func(c *template.ColBuilder) {
				c.Spacer(document.Mm(8))
				c.Text("NOTES:", labelStyle...)
				c.Text(invoiceNotes, bodyStyle...)
			})
		})
	}

	if cfg.ShowNotes && inv.Notes != "" {
		page.AutoRow(func(r *template.RowBuilder) {
			r.Col(12, func(c *template.ColBuilder) {
				c.Spacer(document.Mm(8))
				c.Text("NOTE:", labelStyle...)
				c.Text(inv.Notes, bodyStyle...)
			})
		})
	}

	return doc.Generate()
}

func strIf(cond bool, s string) string {
	if cond {
		return s
	}
	return ""
}

func filterEmpty(lines []string) []string {
	var out []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

func decodeDataURI(dataURI string) ([]byte, bool) {
	if !strings.HasPrefix(dataURI, "data:") {
		return nil, false
	}
	base64Idx := strings.Index(dataURI, ";base64,")
	if base64Idx < 0 {
		return nil, false
	}
	decoded, err := base64.StdEncoding.DecodeString(dataURI[base64Idx+len(";base64,"):])
	if err != nil {
		return nil, false
	}
	return decoded, true
}
