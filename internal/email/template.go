package email

import (
	"fmt"
	"strings"

	"ginvoice/internal/store"
)

// TemplateData holds the variables available in email templates.
type TemplateData struct {
	CompanyName         string
	CompanyWebsite      string
	CompanyPhone        string
	CompanyAddressLine1       string
	CompanyAddressLine2 string
	CompanyPostalCode   string
	CompanyCity         string
	CompanyState        string
	CompanyCountry      string
	OwnerFirstName      string
	OwnerLastName       string
	InvoiceNumber       string
	ClientName          string
	InvoiceTotal        string
	InvoiceDueDate      string
}

// TemplateDataFor builds template variables from an invoice and the company.
func TemplateDataFor(inv store.Invoice, c store.Company) TemplateData {
	return TemplateData{
		CompanyName:         c.Name,
		CompanyWebsite:      c.Website,
		CompanyPhone:        c.Phone,
		CompanyAddressLine1:       c.AddressLine1,
		CompanyAddressLine2: c.AddressLine2,
		CompanyPostalCode:   c.PostalCode,
		CompanyCity:         c.City,
		CompanyState:        c.State,
		CompanyCountry:      c.Country,
		OwnerFirstName:      c.OwnerFirstName,
		OwnerLastName:       c.OwnerLastName,
		InvoiceNumber:       inv.Number,
		ClientName:          inv.Client.Name,
		InvoiceTotal:        fmt.Sprintf("%.2f", float64(inv.Total)/100),
		InvoiceDueDate:      inv.DueDate,
	}
}

// DefaultSubject is used when no subject template is configured.
const DefaultSubject = "{{companyName}} Invoice {{invoiceNumber}}"

// DefaultBody is used when no body template is configured.
const DefaultBody = `<p>Hello 👋</p>
<p>
Please find attached invoice <strong>{{invoiceNumber}}</strong> for {{companyName}} services covering past period.
</p>
<p>It was a pleasure working with you!</p>
<p>Best regards,<br>
{{ownerFirstName}} {{ownerLastName}}<br>
{{companyNameLink}}
</p>`

// RenderTemplate replaces {{variable}} placeholders in a template string
// with values from data. Unknown placeholders are left as-is.
func RenderTemplate(tmpl string, data TemplateData) string {
	companyNameLink := data.CompanyName
	if data.CompanyWebsite != "" {
		companyNameLink = `<a href="` + data.CompanyWebsite + `">` + data.CompanyName + `</a>`
	}
	companyURL := data.CompanyWebsite
	if companyURL != "" {
		companyURL = `<a href="` + companyURL + `">` + companyURL + `</a>`
	}
	r := strings.NewReplacer(
		"{{companyNameLink}}", companyNameLink,
		"{{companyName}}", data.CompanyName,
		"{{companyWebsite}}", data.CompanyWebsite,
		"{{companyURL}}", companyURL,
		"{{companyPhone}}", data.CompanyPhone,
		"{{companyAddressLine1}}", data.CompanyAddressLine1,
		"{{companyAddressLine2}}", data.CompanyAddressLine2,
		"{{companyPostalCode}}", data.CompanyPostalCode,
		"{{companyCity}}", data.CompanyCity,
		"{{companyState}}", data.CompanyState,
		"{{companyCountry}}", data.CompanyCountry,
		"{{ownerFirstName}}", data.OwnerFirstName,
		"{{ownerLastName}}", data.OwnerLastName,
		"{{invoiceNumber}}", data.InvoiceNumber,
		"{{clientName}}", data.ClientName,
		"{{invoiceTotal}}", data.InvoiceTotal,
		"{{invoiceDueDate}}", data.InvoiceDueDate,
	)
	return r.Replace(tmpl)
}
