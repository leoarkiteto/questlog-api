package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"questlog/internal/catalog"
	"questlog/internal/model"
	"questlog/internal/repo"
)

// Server holds dependencies for the HTTP API.
type Server struct {
	store   *repo.Store
	catalog *catalog.Service
}

// New builds the API handler with routes and middleware.
func New(store *repo.Store, catalogService *catalog.Service) http.Handler {
	s := &Server{store: store, catalog: catalogService}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/games", s.handleListGames)
	mux.HandleFunc("POST /api/games", s.handleCreateGame)
	mux.HandleFunc("GET /api/games/{id}", s.handleGetGame)
	mux.HandleFunc("PUT /api/games/{id}", s.handleUpdateGame)
	mux.HandleFunc("DELETE /api/games/{id}", s.handleDeleteGame)
	mux.HandleFunc("GET /api/catalog/search", s.handleCatalogSearch)
	mux.HandleFunc("GET /api/catalog/app/{source}/{appid}", s.handleCatalogApp)

	return s.logMiddleware(s.corsMiddleware(mux))
}

// ---- Middleware ----

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = "*"
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s (%s)", r.Method, r.URL.Path, r.RemoteAddr, time.Since(start).Round(time.Millisecond))
	})
}

// ---- Handlers ----

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "degraded", "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleListGames(w http.ResponseWriter, r *http.Request) {
	var status *model.Status
	if raw := r.URL.Query().Get("status"); raw != "" {
		st := model.Status(raw)
		if !st.Valid() {
			writeError(w, http.StatusBadRequest, "invalid status filter (use wishlist, purchased, playing, played, dropped)")
			return
		}
		status = &st
	}
	games, err := s.store.List(r.Context(), status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load games")
		log.Printf("list games: %v", err)
		return
	}
	writeJSON(w, http.StatusOK, games)
}

func (s *Server) handleCreateGame(w http.ResponseWriter, r *http.Request) {
	var g model.Game
	if !decodeBody(w, r, &g) {
		return
	}
	if err := g.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	created, err := s.store.Create(r.Context(), &g)
	var dup *repo.ErrDuplicate
	if errors.As(err, &dup) {
		writeError(w, http.StatusConflict, duplicateMessage(dup))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create game")
		log.Printf("create game: %v", err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleGetGame(w http.ResponseWriter, r *http.Request) {
	id, ok := gameID(w, r)
	if !ok {
		return
	}
	g, err := s.store.Get(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "game not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load game")
		log.Printf("get game %d: %v", id, err)
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) handleUpdateGame(w http.ResponseWriter, r *http.Request) {
	id, ok := gameID(w, r)
	if !ok {
		return
	}
	var g model.Game
	if !decodeBody(w, r, &g) {
		return
	}
	if err := g.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := s.store.Update(r.Context(), id, &g)
	var dup *repo.ErrDuplicate
	if errors.As(err, &dup) {
		writeError(w, http.StatusConflict, duplicateMessage(dup))
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "game not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update game")
		log.Printf("update game %d: %v", id, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleDeleteGame(w http.ResponseWriter, r *http.Request) {
	id, ok := gameID(w, r)
	if !ok {
		return
	}
	if err := s.store.Delete(r.Context(), id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "game not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete game")
		log.Printf("delete game %d: %v", id, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- Helpers ----

// duplicateMessage explains why a game couldn't be added/renamed: it's
// already in the collection, and the fix is to edit that card's list
// rather than create a second card.
func duplicateMessage(dup *repo.ErrDuplicate) string {
	return fmt.Sprintf("%q is already in your collection as %s — edit that card to change its list.",
		dup.Existing.Title, dup.Existing.Status.Display())
}

func gameID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid game id")
		return 0, false
	}
	return id, true
}

func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
