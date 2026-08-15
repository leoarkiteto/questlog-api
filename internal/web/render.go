package web

import (
	"log"
	"net/http"

	"github.com/a-h/templ"
)

// render writes a templ component as an HTML response.
func render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := c.Render(r.Context(), w); err != nil {
		// Headers are already sent; log is the best we can do.
		log.Printf("render: %v", err)
	}
}

// renderFragment writes a templ component as an HTMX HTML fragment.
func renderFragment(w http.ResponseWriter, r *http.Request, c templ.Component) {
	render(w, r, http.StatusOK, c)
}
