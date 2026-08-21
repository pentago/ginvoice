package store

import (
	"database/sql"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// InvoiceLine is a single line item on an invoice.
type InvoiceLine struct {
	ID          int64
	InvoiceID   int64
	ServiceID   *int64  // nullable
	Description string
	Quantity    float64 // REAL — allows 1.5 hours
	UnitPrice   int64   // integer cents
	LineTotal   int64   // integer cents
	SortOrder   int
}

// Invoice is a row in the invoices table.
type Invoice struct {
	ID        int64
	Number    string
	ClientID  int64
	IssueDate string
	DueDate   string
	Status    string // "draft" | "sent"
	Notes     string
	Subtotal  int64  // integer cents
	TaxRate   int64  // basis points
	TaxAmount int64  // integer cents
	Total     int64  // integer cents
	Currency  string
	SentAt    string
	CreatedAt string
	UpdatedAt string
	// populated by GetInvoiceWithLines:
	Lines  []InvoiceLine
	Client Client
}

// ComputeTotals calculates line totals, subtotal, tax, and total from invoice
// lines and tax rate in basis points. All arithmetic uses integer cents with
// math.Round for half-away-from-zero rounding.
func ComputeTotals(lines []InvoiceLine, taxRateBPS int64) (subtotal, taxAmount, total int64) {
	for i, l := range lines {
		lines[i].LineTotal = int64(math.Round(l.Quantity * float64(l.UnitPrice)))
		subtotal += lines[i].LineTotal
	}
	taxAmount = int64(math.Round(float64(subtotal) * float64(taxRateBPS) / 10000))
	total = subtotal + taxAmount
	return
}

// ParseCentsFromForm converts a decimal currency string ("19.99") to integer
// cents (1999), rounding half away from zero. Unparseable input yields 0.
func ParseCentsFromForm(s string) int64 {
	f, _ := strconv.ParseFloat(s, 64)
	return int64(math.Round(f * 100))
}

// NextInvoiceNumber generates the next sequential invoice number for the given
// prefix and year. The pattern is PREFIX-YYYY-NNN.
func NextInvoiceNumber(db *sql.DB, prefix string, year int) (string, error) {
	var last string
	pattern := fmt.Sprintf("%s-%d-%%", prefix, year)
	err := db.QueryRow(
		`SELECT COALESCE(MAX(number),'') FROM invoices WHERE number LIKE ?`, pattern,
	).Scan(&last)
	if err != nil {
		return "", fmt.Errorf("next invoice number query: %w", err)
	}
	n := 0
	if last != "" {
		parts := strings.Split(last, "-")
		n, _ = strconv.Atoi(parts[len(parts)-1])
	}
	return fmt.Sprintf("%s-%d-%03d", prefix, year, n+1), nil
}

// ListInvoices returns all invoices ordered by number descending.
func ListInvoices(db *sql.DB) ([]Invoice, error) {
	rows, err := db.Query(`
		SELECT id, number, client_id, issue_date, due_date, status, notes,
		       subtotal, tax_rate, tax_amount, total, currency, sent_at,
		       created_at, updated_at
		FROM invoices
		ORDER BY number DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list invoices: %w", err)
	}
	defer rows.Close()

	var invoices []Invoice
	for rows.Next() {
		var inv Invoice
		if err := scanInvoice(rows, &inv); err != nil {
			return nil, fmt.Errorf("scan invoice: %w", err)
		}
		invoices = append(invoices, inv)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate invoices: %w", err)
	}
	return invoices, nil
}

// GetInvoice returns a single invoice by id. Returns sql.ErrNoRows if absent.
func GetInvoice(db *sql.DB, id int64) (Invoice, error) {
	var inv Invoice
	row := db.QueryRow(`
		SELECT id, number, client_id, issue_date, due_date, status, notes,
		       subtotal, tax_rate, tax_amount, total, currency, sent_at,
		       created_at, updated_at
		FROM invoices
		WHERE id = ?`, id)
	if err := scanInvoice(row, &inv); err != nil {
		return Invoice{}, fmt.Errorf("get invoice %d: %w", id, err)
	}
	return inv, nil
}

// GetInvoiceWithLines returns a single invoice with its lines and client
// data populated. Returns sql.ErrNoRows if the invoice does not exist.
func GetInvoiceWithLines(db *sql.DB, id int64) (Invoice, error) {
	inv, err := GetInvoice(db, id)
	if err != nil {
		return Invoice{}, err
	}

	// load lines
	lrows, err := db.Query(`
		SELECT id, invoice_id, service_id, description, quantity, unit_price, line_total, sort_order
		FROM invoice_lines
		WHERE invoice_id = ?
		ORDER BY sort_order, id`, id)
	if err != nil {
		return Invoice{}, fmt.Errorf("get invoice %d lines: %w", id, err)
	}
	defer lrows.Close()

	for lrows.Next() {
		var l InvoiceLine
		var serviceID sql.NullInt64
		if err := lrows.Scan(&l.ID, &l.InvoiceID, &serviceID, &l.Description,
			&l.Quantity, &l.UnitPrice, &l.LineTotal, &l.SortOrder); err != nil {
			return Invoice{}, fmt.Errorf("scan invoice line: %w", err)
		}
		if serviceID.Valid {
			sid := serviceID.Int64
			l.ServiceID = &sid
		}
		inv.Lines = append(inv.Lines, l)
	}
	if err := lrows.Err(); err != nil {
		return Invoice{}, fmt.Errorf("iterate invoice %d lines: %w", id, err)
	}

	// load client
	client, err := GetClient(db, inv.ClientID)
	if err != nil {
		return Invoice{}, fmt.Errorf("get invoice %d client: %w", id, err)
	}
	inv.Client = client

	return inv, nil
}

// CreateInvoice inserts a new invoice and its lines atomically inside a
// transaction. The invoice number and totals must be pre-computed by the caller.
// Returns the inserted invoice id.
func CreateInvoice(db *sql.DB, inv Invoice, lines []InvoiceLine) (int64, error) {
	var invoiceID int64
	err := InTx(db, func(tx *sql.Tx) error {
		res, err := tx.Exec(`
			INSERT INTO invoices (number, client_id, issue_date, due_date, status, notes,
			                      subtotal, tax_rate, tax_amount, total, currency)
			VALUES (?, ?, ?, ?, 'draft', ?, ?, ?, ?, ?, ?)`,
			inv.Number, inv.ClientID, inv.IssueDate, inv.DueDate, inv.Notes,
			inv.Subtotal, inv.TaxRate, inv.TaxAmount, inv.Total, inv.Currency)
		if err != nil {
			return fmt.Errorf("insert invoice: %w", err)
		}
		invoiceID, err = res.LastInsertId()
		if err != nil {
			return fmt.Errorf("invoice last insert id: %w", err)
		}
		for _, l := range lines {
			var sid interface{}
			if l.ServiceID != nil {
				sid = *l.ServiceID
			}
			if _, err := tx.Exec(`
				INSERT INTO invoice_lines (invoice_id, service_id, description, quantity, unit_price, line_total, sort_order)
				VALUES (?, ?, ?, ?, ?, ?, ?)`,
				invoiceID, sid, l.Description, l.Quantity, l.UnitPrice, l.LineTotal, l.SortOrder); err != nil {
				return fmt.Errorf("insert invoice line: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return invoiceID, nil
}

// UpdateInvoice replaces all lines and updates the totals of an existing draft
// invoice atomically. The caller must check that inv.Status == "draft" first.
func UpdateInvoice(db *sql.DB, inv Invoice, lines []InvoiceLine) error {
	return InTx(db, func(tx *sql.Tx) error {
		// update invoice header
		res, err := tx.Exec(`
			UPDATE invoices
			SET issue_date = ?, due_date = ?, notes = ?,
			    subtotal = ?, tax_rate = ?, tax_amount = ?, total = ?,
			    updated_at = datetime('now')
			WHERE id = ? AND status = 'draft'`,
			inv.IssueDate, inv.DueDate, inv.Notes,
			inv.Subtotal, inv.TaxRate, inv.TaxAmount, inv.Total, inv.ID)
		if err != nil {
			return fmt.Errorf("update invoice %d: %w", inv.ID, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("update invoice %d rows affected: %w", inv.ID, err)
		}
		if n == 0 {
			return fmt.Errorf("update invoice %d: %w", inv.ID, sql.ErrNoRows)
		}

		// delete existing lines
		if _, err := tx.Exec(`DELETE FROM invoice_lines WHERE invoice_id = ?`, inv.ID); err != nil {
			return fmt.Errorf("delete invoice %d lines: %w", inv.ID, err)
		}

		// insert new lines
		for _, l := range lines {
			var sid interface{}
			if l.ServiceID != nil {
				sid = *l.ServiceID
			}
			if _, err := tx.Exec(`
				INSERT INTO invoice_lines (invoice_id, service_id, description, quantity, unit_price, line_total, sort_order)
				VALUES (?, ?, ?, ?, ?, ?, ?)`,
				inv.ID, sid, l.Description, l.Quantity, l.UnitPrice, l.LineTotal, l.SortOrder); err != nil {
				return fmt.Errorf("insert invoice %d line: %w", inv.ID, err)
			}
		}
		return nil
	})
}

// invoiceColumns is the shared SELECT column list for invoices.
const invoiceColumns = `id, number, client_id, issue_date, due_date, status, notes,
	subtotal, tax_rate, tax_amount, total, currency, sent_at, created_at, updated_at`

// scanInvoice scans one invoices-table row into inv, mapping SQL NULL columns
// to empty strings.
func scanInvoice(row rowScanner, inv *Invoice) error {
	var dueDate, notes, sentAt sql.NullString
	if err := row.Scan(
		&inv.ID, &inv.Number, &inv.ClientID, &inv.IssueDate,
		&dueDate, &inv.Status, &notes,
		&inv.Subtotal, &inv.TaxRate, &inv.TaxAmount, &inv.Total,
		&inv.Currency, &sentAt, &inv.CreatedAt, &inv.UpdatedAt,
	); err != nil {
		return err
	}
	inv.DueDate = dueDate.String
	inv.Notes = notes.String
	inv.SentAt = sentAt.String
	return nil
}
