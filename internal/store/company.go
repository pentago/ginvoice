package store

import "database/sql"

// Company is the singleton company/profile row (always id=1).
// All money values are integer minor units; DefaultTaxRateBPS is basis points (2000 = 20%).
type Company struct {
	ID                  int64
	Name                string
	OwnerFirstName      string
	OwnerLastName       string
	Website             string
	Address             string
	Email               string
	Phone               string
	LogoData            string
	TaxID               string
	IBAN                string
	DefaultCurrency     string
	DefaultTaxRateBPS   int64
	InvoiceNumberPrefix string
	DefaultEmailSubject string
	DefaultEmailBody    string
	CreatedAt           string
	UpdatedAt           string
}

func UpsertCompany(db *sql.DB, c Company) error {
	_, err := db.Exec(`
		INSERT INTO companies (id, name, owner_first_name, owner_last_name, website, address, email, phone, logo_data, tax_id, iban,
			default_currency, default_tax_rate, invoice_number_prefix, default_email_subject, default_email_body, updated_at)
		VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name, owner_first_name=excluded.owner_first_name, owner_last_name=excluded.owner_last_name,
			website=excluded.website, address=excluded.address, email=excluded.email,
			phone=excluded.phone, logo_data=excluded.logo_data, tax_id=excluded.tax_id,
			iban=excluded.iban, default_currency=excluded.default_currency,
			default_tax_rate=excluded.default_tax_rate,
			invoice_number_prefix=excluded.invoice_number_prefix,
			default_email_subject=excluded.default_email_subject,
			default_email_body=excluded.default_email_body,
			updated_at=excluded.updated_at`,
		c.Name, c.OwnerFirstName, c.OwnerLastName, c.Website, c.Address, c.Email, c.Phone, c.LogoData, c.TaxID, c.IBAN,
		c.DefaultCurrency, c.DefaultTaxRateBPS, c.InvoiceNumberPrefix, c.DefaultEmailSubject, c.DefaultEmailBody)
	return err
}

func GetCompany(db *sql.DB) (Company, bool, error) {
	var c Company
	var ownerFirst, ownerLast, website, logoData, emailSubj, emailBody sql.NullString
	err := db.QueryRow(`SELECT id, name, owner_first_name, owner_last_name, website, address, email, phone, logo_data, tax_id, iban,
		default_currency, default_tax_rate, invoice_number_prefix, default_email_subject, default_email_body, created_at, updated_at
		FROM companies WHERE id=1`).Scan(
		&c.ID, &c.Name, &ownerFirst, &ownerLast, &website, &c.Address, &c.Email, &c.Phone, &logoData, &c.TaxID, &c.IBAN,
		&c.DefaultCurrency, &c.DefaultTaxRateBPS, &c.InvoiceNumberPrefix, &emailSubj, &emailBody, &c.CreatedAt, &c.UpdatedAt)
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
	return c, true, nil
}
