package handlers

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/a-h/templ"

	"ginvoice/internal/store"
	"ginvoice/internal/views"
)

// ClientsHandler serves the clients CRUD pages.
type ClientsHandler struct {
	DB *sql.DB
}

// List handles GET /clients — renders the full clients list page.
func (h *ClientsHandler) List(w http.ResponseWriter, r *http.Request) {
	clients, err := store.ListClients(h.DB)
	if err != nil {
		log.Printf("list clients: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	templ.Handler(views.ClientsPage(clients)).ServeHTTP(w, r)
}

// New handles GET /clients/new — renders a blank client form.
func (h *ClientsHandler) New(w http.ResponseWriter, r *http.Request) {
	templ.Handler(views.NewClientPage()).ServeHTTP(w, r)
}

// Create handles POST /clients — validates and creates a client,
// then redirects back to the list.
func (h *ClientsHandler) Create(w http.ResponseWriter, r *http.Request) {
	c, ok := h.clientFromForm(w, r)
	if !ok {
		return
	}
	if _, err := store.CreateClient(h.DB, c); err != nil {
		log.Printf("create client: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/clients", http.StatusSeeOther)
}

// Edit handles GET /clients/{id}/edit — renders the form pre-filled.
func (h *ClientsHandler) Edit(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	c, err := store.GetClient(h.DB, id)
	if err != nil {
		h.clientError(w, r, err)
		return
	}
	templ.Handler(views.EditClientPage(c)).ServeHTTP(w, r)
}

// Update handles POST /clients/{id} — validates and updates the client,
// then redirects back to the list.
func (h *ClientsHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	// Check existence first so a missing record returns 404, not 400.
	if _, err := store.GetClient(h.DB, id); err != nil {
		h.clientError(w, r, err)
		return
	}
	c, ok := h.clientFromForm(w, r)
	if !ok {
		return
	}
	c.ID = id
	if err := store.UpdateClient(h.DB, c); err != nil {
		h.clientError(w, r, err)
		return
	}
	http.Redirect(w, r, "/clients", http.StatusSeeOther)
}

// Delete handles POST /clients/{id}/delete — deletes the client or
// returns 409 when it is referenced by invoices. For HTMX requests an
// empty 200 response swap-removes the row; otherwise it redirects.
func (h *ClientsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	if err := store.DeleteClient(h.DB, id); err != nil {
		if errors.Is(err, store.ErrClientReferenced) {
			http.Error(w, "client is referenced by invoices", http.StatusConflict)
			return
		}
		h.clientError(w, r, err)
		return
	}

	if r.Header.Get("HX-Request") != "" {
		w.WriteHeader(http.StatusOK) // empty body: hx-swap="outerHTML" removes the row
		return
	}
	http.Redirect(w, r, "/clients", http.StatusSeeOther)
}

// pathID parses the {id} path value; writes 404 on malformed input.
func (h *ClientsHandler) pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return 0, false
	}
	return id, true
}

// clientFromForm parses the client fields from the request body;
// writes 400 on malformed bodies or missing name.
func (h *ClientsHandler) clientFromForm(w http.ResponseWriter, r *http.Request) (store.Client, bool) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return store.Client{}, false
	}
	c := store.Client{
		Name:         strings.TrimSpace(r.FormValue("name")),
		CompanyName:  strings.TrimSpace(r.FormValue("company_name")),
		Address:      strings.TrimSpace(r.FormValue("address")),
		Email:        strings.TrimSpace(r.FormValue("email")),
		Phone:        strings.TrimSpace(r.FormValue("phone")),
		TaxID:        strings.TrimSpace(r.FormValue("tax_id")),
		EmailSubject: r.FormValue("email_subject"),
		EmailBody:    r.FormValue("email_body"),
	}
	if c.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return store.Client{}, false
	}
	return c, true
}

// clientError maps store errors to responses: 404 for missing rows, 500 otherwise.
func (h *ClientsHandler) clientError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	log.Printf("clients handler: %v", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}
