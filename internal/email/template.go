package email

import "strings"

// TemplateData holds the variables available in email templates.
type TemplateData struct {
	CompanyName    string
	CompanyWebsite string
	CompanyPhone   string
	OwnerFirstName string
	OwnerLastName  string
	InvoiceNumber  string
	ClientName     string
	InvoiceTotal   string
	InvoiceDueDate string
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
		"{{ownerFirstName}}", data.OwnerFirstName,
		"{{ownerLastName}}", data.OwnerLastName,
		"{{invoiceNumber}}", data.InvoiceNumber,
		"{{clientName}}", data.ClientName,
		"{{invoiceTotal}}", data.InvoiceTotal,
		"{{invoiceDueDate}}", data.InvoiceDueDate,
	)
	return r.Replace(tmpl)
}
