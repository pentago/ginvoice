package main

	import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"


	"ginvoice/internal/config"
	_ "ginvoice/internal/deps"
	"ginvoice/internal/email"
	"ginvoice/internal/handlers"
	"ginvoice/internal/middleware"
	"ginvoice/internal/store"
	"ginvoice/migrations"
	webassets "ginvoice/web"
)

const (
	dataDir = "/data"
	dbPath   = "/data/ginvoice.db"
)


func main() {
	cfg := config.Load()

	db, err := openDB()
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("close db: %v", err)
		}
	}()

	if err := store.RunMigrations(db, migrations.FS); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	staticFS, err := webassets.Static()
	if err != nil {
		log.Fatalf("static fs: %v", err)
	}

	mux := http.NewServeMux()
	// healthz stays unauthenticated
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := db.PingContext(r.Context()); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, `{"status":"error"}`)
			return
		}

		writeJSON(w, http.StatusOK, `{"status":"ok"}`)
	})

	// all other routes go through auth
	protected := http.NewServeMux()
	protected.Handle(
		"GET /static/",
		http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))),
	)
	ch := &handlers.CompanyHandler{DB: db, Cfg: cfg}
	protected.HandleFunc("GET /settings", ch.ShowSettings)
	protected.HandleFunc("POST /settings", ch.SaveSettings)
	sender := &email.ResendSender{
		APIKey:  cfg.ResendAPIKey,
		BaseURL: cfg.ResendBaseURL,
		From:    fmt.Sprintf("%s <%s>", cfg.FromName, cfg.FromEmail),
	}
	ih := &handlers.InvoicesHandler{DB: db, Cfg: cfg, Sender: sender}
	clh := &handlers.ClientsHandler{DB: db}
	protected.HandleFunc("GET /clients", clh.List)
	protected.HandleFunc("GET /clients/new", clh.New)
	protected.HandleFunc("POST /clients", clh.Create)
	protected.HandleFunc("GET /clients/{id}/edit", clh.Edit)
	protected.HandleFunc("POST /clients/{id}", clh.Update)
	protected.HandleFunc("POST /clients/{id}/delete", clh.Delete)
	svh := &handlers.ServicesHandler{DB: db}
	protected.HandleFunc("GET /services", svh.List)
	protected.HandleFunc("GET /services/new", svh.New)
	protected.HandleFunc("POST /services", svh.Create)
	protected.HandleFunc("GET /services/{id}/edit", svh.Edit)
	protected.HandleFunc("POST /services/{id}", svh.Update)
	protected.HandleFunc("POST /services/{id}/delete", svh.Delete)
	protected.HandleFunc("GET /invoices", ih.List)
	protected.HandleFunc("GET /invoices/new", ih.New)
	protected.HandleFunc("GET /invoices/line-item", ih.LineItem)
	protected.HandleFunc("POST /invoices", ih.Create)
	protected.HandleFunc("GET /invoices/{id}", ih.View)
	protected.HandleFunc("GET /invoices/{id}/edit", ih.Edit)
	protected.HandleFunc("POST /invoices/{id}", ih.Update)
	protected.HandleFunc("GET /invoices/{id}/pdf", ih.DownloadPDF)
	protected.HandleFunc("POST /invoices/{id}/save", ih.SavePDF)
	protected.HandleFunc("POST /invoices/{id}/email", ih.SendEmail)
	authMiddleware := middleware.Auth(cfg.TrustedProxyCIDR, cfg.AuthHeader)
	protected.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/invoices", http.StatusFound)
	})
	mux.Handle("/", authMiddleware(protected))

	log.Printf("starting on %s", cfg.Addr)
	if err := http.ListenAndServe(cfg.Addr, mux); err != nil {
		log.Fatalf("serve: %v", err)
	}
}

func openDB() (*sql.DB, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create db dir %s: %w", dataDir, err)
	}

	db, err := store.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", dbPath, err)
	}

	return db, nil
}


func writeJSON(w http.ResponseWriter, statusCode int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if _, err := w.Write([]byte(body)); err != nil {
		log.Printf("write response: %v", err)
	}
}
