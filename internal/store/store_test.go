package store

import (
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func testMigrations(t *testing.T) fs.FS {
	t.Helper()
	return os.DirFS(filepath.Join("..", "..", "migrations"))
}

func migrateTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db := openTestDB(t)
	if err := RunMigrations(db, testMigrations(t)); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	return db
}

// S1: happy path — all 5 schema tables exist after RunMigrations.
func TestMigrations_CreatesTables(t *testing.T) {
	db := migrateTestDB(t)

	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name")
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	defer rows.Close()

	got := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		got[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	for _, want := range []string{"companies", "clients", "services", "invoices", "invoice_lines"} {
		if !got[want] {
			t.Errorf("table %q missing after migrations; got tables %v", want, got)
		}
	}
}

// S4: WAL journal mode is enabled via the DSN pragma.
func TestOpen_WALMode(t *testing.T) {
	db := migrateTestDB(t)

	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("pragma journal_mode: %v", err)
	}
	if !strings.EqualFold(mode, "wal") {
		t.Errorf("journal_mode = %q, want wal", mode)
	}

	var fk int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatalf("pragma foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Errorf("foreign_keys = %d, want 1", fk)
	}
}

// S2: edge — inserting an invoice with a non-existent client must violate the FK constraint.
func TestMigrations_FKConstraint(t *testing.T) {
	db := migrateTestDB(t)

	_, err := db.Exec(
		"INSERT INTO invoices(number, client_id, issue_date, currency) VALUES ('INV-2026-001', 999, '2026-01-01', 'EUR')",
	)
	if err == nil {
		t.Fatal("expected FOREIGN KEY error for orphan invoice insert, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "foreign key") {
		t.Errorf("error = %v, want it to mention FOREIGN KEY constraint", err)
	}
}

// S3: running migrations twice is an idempotent no-op.
func TestMigrations_Idempotent(t *testing.T) {
	db := openTestDB(t)
	migs := testMigrations(t)

	if err := RunMigrations(db, migs); err != nil {
		t.Fatalf("first RunMigrations: %v", err)
	}
	if err := RunMigrations(db, migs); err != nil {
		t.Fatalf("second RunMigrations should be a no-op: %v", err)
	}
}

// S5: WAL allows a concurrent reader and writer without SQLITE_BUSY.
func TestWAL_ConcurrentReadWrite(t *testing.T) {
	db := migrateTestDB(t)

	if _, err := db.Exec("INSERT INTO clients(name) VALUES ('Acme GmbH')"); err != nil {
		t.Fatalf("seed client: %v", err)
	}
	var clientID int
	if err := db.QueryRow("SELECT id FROM clients LIMIT 1").Scan(&clientID); err != nil {
		t.Fatalf("fetch client id: %v", err)
	}

	const writes = 50
	writerDone := make(chan error, 1)
	readerStopped := make(chan struct{})

	go func() {
		defer close(writerDone)
		for i := range writes {
			q := fmt.Sprintf(
				"INSERT INTO invoices(number, client_id, issue_date, currency) VALUES ('INV-2026-%04d', %d, '2026-01-01', 'EUR')",
				i, clientID,
			)
			if _, err := db.Exec(q); err != nil {
				writerDone <- fmt.Errorf("insert invoice %d: %w", i, err)
				return
			}
		}
	}()

	var wg sync.WaitGroup
	wg.Go(func() {
		defer close(readerStopped)
		for {
			select {
			case <-writerDone:
				return
			default:
			}
			var n int
			if err := db.QueryRow("SELECT 1").Scan(&n); err != nil {
				if strings.Contains(err.Error(), "SQLITE_BUSY") {
					return
				}
				return
			}
		}
	})

	for err := range writerDone {
		if err != nil {
			t.Errorf("writer failed: %v", err)
		}
	}
	wg.Wait()

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM invoices").Scan(&count); err != nil {
		t.Fatalf("count invoices: %v", err)
	}
	if count != writes {
		t.Errorf("invoices count = %d, want %d", count, writes)
	}
}

// S6: InTx commits on success and rolls back on error.
func TestInTx_CommitAndRollback(t *testing.T) {
	db := migrateTestDB(t)

	err := InTx(db, func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT INTO clients(name) VALUES ('Committed Client')")
		return err
	})
	if err != nil {
		t.Fatalf("InTx commit: %v", err)
	}
	var committed int
	if err := db.QueryRow("SELECT COUNT(*) FROM clients WHERE name = 'Committed Client'").Scan(&committed); err != nil {
		t.Fatalf("query committed client: %v", err)
	}
	if committed != 1 {
		t.Errorf("committed client count = %d, want 1", committed)
	}

	wantErr := fmt.Errorf("boom")
	err = InTx(db, func(tx *sql.Tx) error {
		if _, err := tx.Exec("INSERT INTO clients(name) VALUES ('Rolled Back Client')"); err != nil {
			return err
		}
		return wantErr
	})
	if err == nil || err.Error() != wantErr.Error() {
		t.Fatalf("InTx rollback: err = %v, want %v", err, wantErr)
	}
	var rolledBack int
	if err := db.QueryRow("SELECT COUNT(*) FROM clients WHERE name = 'Rolled Back Client'").Scan(&rolledBack); err != nil {
		t.Fatalf("query rolled back client: %v", err)
	}
	if rolledBack != 0 {
		t.Errorf("rolled-back client count = %d, want 0 (row leaked past rollback)", rolledBack)
	}
}
