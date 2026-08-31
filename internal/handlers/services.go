package handlers

import (
	"database/sql"
	"errors"
	"log"
	"math"
	"net/http"
	"strconv"

	"ginvoice/internal/store"
	"ginvoice/internal/views"
)

// ServicesHandler serves the services catalog CRUD routes. Services are
// reusable line-item templates, not invoice-bound records.
type ServicesHandler struct {
	DB *sql.DB
}

// parseCents converts a decimal price string ("19.99") into integer cents
// (1999), rounding half away from zero. Unparseable input yields 0; required
// fields are validated separately by the caller.
func parseCents(s string) int64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return int64(math.Round(f * 100))
}

// List renders the services catalog.
func (h *ServicesHandler) List(w http.ResponseWriter, r *http.Request) {
	services, err := store.ListServices(h.DB)
	if err != nil {
		log.Printf("list services: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := views.ServicesPage(services).Render(r.Context(), w); err != nil {
		log.Printf("render services page: %v", err)
	}
}

// New renders a blank create form.
func (h *ServicesHandler) New(w http.ResponseWriter, r *http.Request) {
	if err := views.NewServicePage().Render(r.Context(), w); err != nil {
		log.Printf("render new service page: %v", err)
	}
}

// Create parses the form (price as decimal string -> integer cents) and
// persists a new service.
func (h *ServicesHandler) Create(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	s := serviceFromForm(r)
	if s.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if _, err := store.CreateService(h.DB, s); err != nil {
		log.Printf("create service: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/services", http.StatusSeeOther)
}

// Edit renders the update form pre-filled from the stored service.
func (h *ServicesHandler) Edit(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	s, err := store.GetService(h.DB, id)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "service not found", http.StatusNotFound)
		return
	}
	if err != nil {
		log.Printf("get service %d: %v", id, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := views.EditServicePage(s).Render(r.Context(), w); err != nil {
		log.Printf("render edit service page: %v", err)
	}
}

// Update parses the form and rewrites the stored service.
func (h *ServicesHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	s := serviceFromForm(r)
	s.ID = id
	if s.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if err := store.UpdateService(h.DB, s); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "service not found", http.StatusNotFound)
			return
		}
		log.Printf("update service %d: %v", id, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/services", http.StatusSeeOther)
}

// Delete removes an unreferenced service; referenced deletes return 409 so
// historical invoice lines are never disturbed.
func (h *ServicesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	err := store.DeleteService(h.DB, id)
	switch {
	case err == nil:
		http.Redirect(w, r, "/services", http.StatusSeeOther)
	case errors.Is(err, store.ErrServiceReferenced):
		http.Error(w, store.ErrServiceReferenced.Error(), http.StatusConflict)
	case errors.Is(err, sql.ErrNoRows):
		http.Error(w, "service not found", http.StatusNotFound)
	default:
		log.Printf("delete service %d: %v", id, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// serviceFromForm builds a store.Service from posted form values. The price
// arrives as a decimal string and is converted to integer cents here; unit
// falls back to the schema default "item" when blank.
func serviceFromForm(r *http.Request) store.Service {
	unit := r.PostFormValue("unit")
	if unit == "" {
		unit = "item"
	}
	return store.Service{
		Name:             r.PostFormValue("name"),
		DefaultUnitPrice: parseCents(r.PostFormValue("unit_price")),
		Unit:             unit,
	}
}

// pathID parses the {id} path segment, writing an error response when invalid.
func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "invalid service id", http.StatusBadRequest)
		return 0, false
	}
	return id, true
}
