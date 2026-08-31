package store

import "database/sql"

// DefaultInvoiceNotes is used when neither client nor company sets invoice notes.
const DefaultInvoiceNotes = `Payment due date is 7 days.
If needed when making the payment, use the full invoice number as the reference number.
The document is valid without a stamp and signature.
Place of issue: {{companyCity}},{{companyCountry}},`

// Company is the singleton company/profile row (always id=1).
// All money values are integer minor units; DefaultTaxRateBPS is basis points (2000 = 20%).
type Company struct {
	ID                  int64
	Name                string
	OwnerFirstName      string
	OwnerLastName       string
	Website             string
	AddressLine1        string
	AddressLine2        string
	PostalCode          string
	City                string
	State               string
	Country             string
	Email               string
	Phone               string
	LogoData            string
	TaxID               string
	IBAN                string
	DefaultTaxRateBPS   int64
	DefaultEmailSubject string
	DefaultEmailBody    string
	InvoiceNotes        string
	PdfConfig           string
	CreatedAt           string
	UpdatedAt           string
}

func UpsertCompany(db *sql.DB, c Company) error {
	_, err := db.Exec(`
		INSERT INTO companies (id, name, owner_first_name, owner_last_name, website, address_line1, address_line2, postal_code, city, state, country, email, phone, logo_data, tax_id, iban,
			default_tax_rate, default_email_subject, default_email_body, invoice_notes, pdf_config, updated_at)
		VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name, owner_first_name=excluded.owner_first_name, owner_last_name=excluded.owner_last_name,
			website=excluded.website, address_line1=excluded.address_line1, address_line2=excluded.address_line2,
			postal_code=excluded.postal_code, city=excluded.city, state=excluded.state, country=excluded.country,
			email=excluded.email,
			phone=excluded.phone, logo_data=excluded.logo_data, tax_id=excluded.tax_id,
			iban=excluded.iban,
			default_tax_rate=excluded.default_tax_rate,
			default_email_subject=excluded.default_email_subject,
			default_email_body=excluded.default_email_body,
			invoice_notes=excluded.invoice_notes,
			pdf_config=excluded.pdf_config,
			updated_at=excluded.updated_at`,
		c.Name, c.OwnerFirstName, c.OwnerLastName, c.Website, c.AddressLine1, c.AddressLine2, c.PostalCode, c.City, c.State, c.Country,
		c.Email, c.Phone, c.LogoData, c.TaxID, c.IBAN,
		c.DefaultTaxRateBPS, c.DefaultEmailSubject, c.DefaultEmailBody, c.InvoiceNotes, c.PdfConfig)
	return err
}

func GetCompany(db *sql.DB) (Company, bool, error) {
	var c Company
	var ownerFirst, ownerLast, website, logoData, emailSubj, emailBody, invoiceNotes, pdfCfg sql.NullString
	err := db.QueryRow(`SELECT id, name, owner_first_name, owner_last_name, website, address_line1, address_line2, postal_code, city, state, country, email, phone, logo_data, tax_id, iban,
		default_tax_rate, default_email_subject, default_email_body, invoice_notes, pdf_config, created_at, updated_at
		FROM companies WHERE id=1`).Scan(
		&c.ID, &c.Name, &ownerFirst, &ownerLast, &website, &c.AddressLine1, &c.AddressLine2, &c.PostalCode, &c.City, &c.State, &c.Country,
		&c.Email, &c.Phone, &logoData, &c.TaxID, &c.IBAN,
		&c.DefaultTaxRateBPS, &emailSubj, &emailBody, &invoiceNotes, &pdfCfg, &c.CreatedAt, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return Company{}, false, nil
	}
	if err != nil {
		return Company{}, false, err
	}
	c.OwnerFirstName = ownerFirst.String
	c.OwnerLastName = ownerLast.String
	c.Website = website.String
	c.LogoData = logoData.String
	c.DefaultEmailSubject = emailSubj.String
	c.DefaultEmailBody = emailBody.String
	c.InvoiceNotes = invoiceNotes.String
	c.PdfConfig = pdfCfg.String
	return c, true, nil
}
