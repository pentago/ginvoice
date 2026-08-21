package store

import (
	"database/sql"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

// Open opens the SQLite database with required pragmas.
// dsn is a plain file path; pragmas are appended automatically.
func Open(dsn string) (*sql.DB, error) {
	fullDSN := "file:" + dsn +
		"?_pragma=foreign_keys(1)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=journal_mode(WAL)"

	db, err := sql.Open("sqlite", fullDSN)
	if err != nil {
		return nil, fmt.Errorf("sql open: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return db, nil
}

// RunMigrations applies all migrations found in migrationsFS using goose.
// migrationsFS must be rooted at the directory containing the *.sql files,
// e.g. migrations.FS or os.DirFS("migrations").
func RunMigrations(db *sql.DB, migrationsFS fs.FS) error {
	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("goose dialect: %w", err)
	}
	if err := goose.Up(db, "."); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}

// InTx executes fn inside a transaction; rolls back on error.
func InTx(db *sql.DB, fn func(*sql.Tx) error) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
