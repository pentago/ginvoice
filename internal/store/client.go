package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// ErrClientReferenced is returned by DeleteClient when the client is
// referenced by one or more invoices. Invoices are immutable history,
// so the delete must be blocked instead of cascading.
var ErrClientReferenced = errors.New("client is referenced by invoices")

// Client is a row in the clients table.
type Client struct {
	ID           int64
	Name         string
	CompanyName  string
	Address      string
	Email        string
	Phone        string
	TaxID        string
	EmailSubject string
	EmailBody    string
	CreatedAt    string
	UpdatedAt    string
}

func ListClients(db *sql.DB) ([]Client, error) {
	rows, err := db.Query(`
		SELECT id, name, company_name, address, email, phone, tax_id, email_subject, email_body, created_at, updated_at
		FROM clients
		ORDER BY name COLLATE NOCASE, id`)
	if err != nil {
		return nil, fmt.Errorf("list clients: %w", err)
	}
	defer rows.Close()

	var clients []Client
	for rows.Next() {
		var c Client
		if err := scanClient(rows, &c); err != nil {
			return nil, fmt.Errorf("scan client: %w", err)
		}
		clients = append(clients, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate clients: %w", err)
	}
	return clients, nil
}

func GetClient(db *sql.DB, id int64) (Client, error) {
	var c Client
	row := db.QueryRow(`
		SELECT id, name, company_name, address, email, phone, tax_id, email_subject, email_body, created_at, updated_at
		FROM clients
		WHERE id = ?`, id)
	if err := scanClient(row, &c); err != nil {
		return Client{}, fmt.Errorf("get client %d: %w", id, err)
	}
	return c, nil
}

func CreateClient(db *sql.DB, c Client) (int64, error) {
	res, err := db.Exec(`
		INSERT INTO clients (name, company_name, address, email, phone, tax_id, email_subject, email_body)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		c.Name, c.CompanyName, c.Address, c.Email, c.Phone, c.TaxID, c.EmailSubject, c.EmailBody)
	if err != nil {
		return 0, fmt.Errorf("create client: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("create client: last insert id: %w", err)
	}
	return id, nil
}

func UpdateClient(db *sql.DB, c Client) error {
	res, err := db.Exec(`
		UPDATE clients
		SET name = ?, company_name = ?, address = ?, email = ?, phone = ?, tax_id = ?,
		    email_subject = ?, email_body = ?,
		    updated_at = datetime('now')
		WHERE id = ?`,
		c.Name, c.CompanyName, c.Address, c.Email, c.Phone, c.TaxID, c.EmailSubject, c.EmailBody, c.ID)
	if err != nil {
		return fmt.Errorf("update client %d: %w", c.ID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update client %d: rows affected: %w", c.ID, err)
	}
	if n == 0 {
		return fmt.Errorf("update client %d: %w", c.ID, sql.ErrNoRows)
	}
	return nil
}

// DeleteClient deletes a client by id unless it is referenced by invoices,
// in which case it returns ErrClientReferenced and deletes nothing.
// Returns sql.ErrNoRows if the client does not exist.
func DeleteClient(db *sql.DB, id int64) error {
	var refs int
	if err := db.QueryRow(`SELECT COUNT(*) FROM invoices WHERE client_id = ?`, id).Scan(&refs); err != nil {
		return fmt.Errorf("check client %d references: %w", id, err)
	}
	if refs > 0 {
		return fmt.Errorf("delete client %d: %w", id, ErrClientReferenced)
	}

	res, err := db.Exec(`DELETE FROM clients WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete client %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete client %d: rows affected: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("delete client %d: %w", id, sql.ErrNoRows)
	}
	return nil
}

// rowScanner abstracts *sql.Row and *sql.Rows for shared column scanning.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanClient(row rowScanner, c *Client) error {
	var companyName, address, email, phone, taxID, emailSubject, emailBody sql.NullString
	if err := row.Scan(
		&c.ID, &c.Name, &companyName, &address,
		&email, &phone, &taxID,
		&emailSubject, &emailBody,
		&c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		return err
	}
	c.CompanyName = companyName.String
	c.Address = address.String
	c.Email = email.String
	c.Phone = phone.String
	c.TaxID = taxID.String
	c.EmailSubject = emailSubject.String
	c.EmailBody = emailBody.String
	return nil
}
