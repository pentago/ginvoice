package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// ErrServiceReferenced is returned by DeleteService when one or more
// invoice_lines rows reference the service. Historical lines are never
// modified by the app layer; callers surface this as HTTP 409 Conflict.
var ErrServiceReferenced = errors.New("service is referenced by invoice lines")

// Service is a reusable line-item template shown in the services catalog.
// DefaultUnitPrice is integer cents (e.g. 5000 = 50.00); Unit is a free-form
// label such as "hour", "day" or "item".
type Service struct {
	ID               int64
	Name             string
	Description      string
	DefaultUnitPrice int64 // integer cents
	Unit             string
	CreatedAt        string
	UpdatedAt        string
}

const serviceColumns = "id, name, description, default_unit_price, unit, created_at, updated_at"

// scanService scans one services row. description/unit/timestamps are nullable
// columns, so they are read via sql.NullString.
func scanService(sc interface{ Scan(dest ...any) error }) (Service, error) {
	var (
		s           Service
		description sql.NullString
		unit        sql.NullString
		createdAt   sql.NullString
		updatedAt   sql.NullString
	)
	if err := sc.Scan(&s.ID, &s.Name, &description, &s.DefaultUnitPrice, &unit, &createdAt, &updatedAt); err != nil {
		return Service{}, err
	}
	s.Description = description.String
	s.Unit = unit.String
	s.CreatedAt = createdAt.String
	s.UpdatedAt = updatedAt.String
	return s, nil
}

// ListServices returns all catalog services in insertion order.
func ListServices(db *sql.DB) ([]Service, error) {
	rows, err := db.Query("SELECT " + serviceColumns + " FROM services ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	defer rows.Close()

	services := []Service{}
	for rows.Next() {
		s, err := scanService(rows)
		if err != nil {
			return nil, fmt.Errorf("scan service: %w", err)
		}
		services = append(services, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate services: %w", err)
	}
	return services, nil
}

// GetService fetches one service by id; returns sql.ErrNoRows when missing.
func GetService(db *sql.DB, id int64) (Service, error) {
	row := db.QueryRow("SELECT "+serviceColumns+" FROM services WHERE id = ?", id)
	s, err := scanService(row)
	if err != nil {
		return Service{}, fmt.Errorf("get service %d: %w", id, err)
	}
	return s, nil
}

// CreateService inserts a new catalog service and returns its id.
func CreateService(db *sql.DB, s Service) (int64, error) {
	res, err := db.Exec(
		"INSERT INTO services (name, description, default_unit_price, unit) VALUES (?, ?, ?, ?)",
		s.Name, s.Description, s.DefaultUnitPrice, s.Unit,
	)
	if err != nil {
		return 0, fmt.Errorf("insert service: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("service last insert id: %w", err)
	}
	return id, nil
}

// UpdateService rewrites an existing service; returns sql.ErrNoRows when the
// id does not exist.
func UpdateService(db *sql.DB, s Service) error {
	res, err := db.Exec(
		"UPDATE services SET name = ?, description = ?, default_unit_price = ?, unit = ?, updated_at = datetime('now') WHERE id = ?",
		s.Name, s.Description, s.DefaultUnitPrice, s.Unit, s.ID,
	)
	if err != nil {
		return fmt.Errorf("update service %d: %w", s.ID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update service %d rows affected: %w", s.ID, err)
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteService removes an unreferenced service. If any invoice_lines row
// references the service it returns ErrServiceReferenced and deletes nothing;
// historical lines are never modified. Returns sql.ErrNoRows when the service
// does not exist.
func DeleteService(db *sql.DB, id int64) error {
	var refs int
	if err := db.QueryRow("SELECT COUNT(*) FROM invoice_lines WHERE service_id = ?", id).Scan(&refs); err != nil {
		return fmt.Errorf("check service %d references: %w", id, err)
	}
	if refs > 0 {
		return ErrServiceReferenced
	}

	res, err := db.Exec("DELETE FROM services WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete service %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete service %d rows affected: %w", id, err)
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
