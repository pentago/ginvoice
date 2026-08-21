-- +goose Up
CREATE TABLE companies (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    address TEXT,
    email TEXT,
    phone TEXT,
    logo_path TEXT,
    tax_id TEXT,
    iban TEXT,
    default_currency TEXT DEFAULT 'EUR',
    default_tax_rate INTEGER DEFAULT 0,
    invoice_number_prefix TEXT DEFAULT 'INV',
    created_at TEXT DEFAULT (datetime('now')),
    updated_at TEXT DEFAULT (datetime('now'))
);

CREATE TABLE clients (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    company_name TEXT,
    address TEXT,
    email TEXT,
    phone TEXT,
    tax_id TEXT,
    created_at TEXT DEFAULT (datetime('now')),
    updated_at TEXT DEFAULT (datetime('now'))
);

CREATE TABLE services (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,
    default_unit_price INTEGER NOT NULL,
    unit TEXT DEFAULT 'item',
    created_at TEXT DEFAULT (datetime('now')),
    updated_at TEXT DEFAULT (datetime('now'))
);

CREATE TABLE invoices (
    id INTEGER PRIMARY KEY,
    number TEXT NOT NULL UNIQUE,
    client_id INTEGER NOT NULL REFERENCES clients(id),
    issue_date TEXT NOT NULL,
    due_date TEXT,
    status TEXT NOT NULL DEFAULT 'draft',
    notes TEXT,
    subtotal INTEGER NOT NULL DEFAULT 0,
    tax_rate INTEGER NOT NULL DEFAULT 0,
    tax_amount INTEGER NOT NULL DEFAULT 0,
    total INTEGER NOT NULL DEFAULT 0,
    currency TEXT NOT NULL,
    sent_at TEXT,
    created_at TEXT DEFAULT (datetime('now')),
    updated_at TEXT DEFAULT (datetime('now'))
);

CREATE TABLE invoice_lines (
    id INTEGER PRIMARY KEY,
    invoice_id INTEGER NOT NULL REFERENCES invoices(id) ON DELETE CASCADE,
    service_id INTEGER REFERENCES services(id) ON DELETE SET NULL,
    description TEXT NOT NULL,
    quantity REAL NOT NULL,
    unit_price INTEGER NOT NULL,
    line_total INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TEXT DEFAULT (datetime('now'))
);

CREATE UNIQUE INDEX idx_invoices_number ON invoices(number);
CREATE INDEX idx_invoices_client_id ON invoices(client_id);
CREATE INDEX idx_invoice_lines_invoice_id ON invoice_lines(invoice_id);
CREATE INDEX idx_invoice_lines_service_id ON invoice_lines(service_id);

-- +goose Down
DROP INDEX IF EXISTS idx_invoice_lines_service_id;
DROP INDEX IF EXISTS idx_invoice_lines_invoice_id;
DROP INDEX IF EXISTS idx_invoices_client_id;
DROP INDEX IF EXISTS idx_invoices_number;
DROP TABLE IF EXISTS invoice_lines;
DROP TABLE IF EXISTS invoices;
DROP TABLE IF EXISTS services;
DROP TABLE IF EXISTS clients;
DROP TABLE IF EXISTS companies;
