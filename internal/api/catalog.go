package api

import (
	"log"
	"net/http"
	"strconv"
)

// handleCatalogSearch queries the merged game catalog (Steam + RAWG):
// GET /api/catalog/search?q=term
func (s *Server) handleCatalogSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "missing query parameter q")
		return
	}
	results := s.catalog.Search(r.Context(), q)
	if len(results) == 0 {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, results)
}

// handleCatalogApp returns enriched data for one catalog entry:
// GET /api/catalog/app/{source}/{appid}   (source: steam | rawg)
func (s *Server) handleCatalogApp(w http.ResponseWriter, r *http.Request) {
	source := r.PathValue("source")
	appID, err := strconv.ParseInt(r.PathValue("appid"), 10, 64)
	if err != nil || appID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid app id")
		return
	}
	details, err := s.catalog.Details(r.Context(), source, appID)
	if err != nil {
		log.Printf("catalog app %s/%d: %v", source, appID, err)
		writeError(w, http.StatusBadGateway, "catalog lookup failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, details)
}
