package web

import (
	"context"
	"errors"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/leoarkiteto/questlog-api/internal/catalog"
	"github.com/leoarkiteto/questlog-api/internal/model"
	"github.com/leoarkiteto/questlog-api/internal/repo"
)

// Server holds the dependencies for the HTML UI.
type Server struct {
	store   *repo.Store
	catalog *catalog.Service
}

// New builds the HTTP handler for the whole site: HTML pages, HTMX
// partials, and the health check.
func New(store *repo.Store, catalogService *catalog.Service) http.Handler {
	s := &Server{store: store, catalog: catalogService}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleDashboard)
	mux.HandleFunc("GET /library", s.handleLibrary)
	mux.HandleFunc("GET /search", s.handleSearch)
	mux.HandleFunc("GET /games/new", s.handleNewForm)
	mux.HandleFunc("POST /games", s.handleCreate)
	mux.HandleFunc("GET /games/{id}", s.handleDetail)
	mux.HandleFunc("GET /games/{id}/edit", s.handleEditForm)
	mux.HandleFunc("POST /games/{id}", s.handleUpdate)
	mux.HandleFunc("POST /games/{id}/delete", s.handleDelete)
	mux.HandleFunc("POST /games/{id}/enrich", s.handleEnrich)
	mux.HandleFunc("GET /partials/catalog/search", s.handleCatalogSearch)
	mux.HandleFunc("GET /partials/catalog/app/{source}/{appid}", s.handleCatalogApp)
	mux.HandleFunc("GET /health", s.handleHealth)

	return s.logMiddleware(mux)
}

// ---- Middleware ----

func (s *Server) logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		log.Printf("%s %s (%s)", r.Method, r.URL.Path, r.RemoteAddr)
	})
}

// ---- Handlers ----

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		http.Error(w, "degraded: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	games, err := s.store.List(r.Context(), nil)
	if err != nil {
		serverError(w, err)
		return
	}
	view := DashboardView{
		Featured: pickFeatured(games),
		Rows:     buildRows(games),
	}
	render(w, r, http.StatusOK, Layout("Questlog — my games", r.URL.Path, "", DashboardPage(view)))
}

func (s *Server) handleLibrary(w http.ResponseWriter, r *http.Request) {
	games, err := s.store.List(r.Context(), nil)
	if err != nil {
		serverError(w, err)
		return
	}

	q := r.URL.Query()
	filter := q.Get("filter")
	platform := strings.TrimSpace(q.Get("platform"))
	sortKey := q.Get("sort")
	if filter == "" {
		filter = "all"
	}
	if !validFilter(filter) {
		filter = "all"
	}
	if sortKey == "" {
		sortKey = "recent"
	}
	if !validSort(sortKey) {
		sortKey = "recent"
	}

	platforms, counts := platformSummary(games)
	shown := filterSortGames(games, filter, platform, sortKey)

	active := 0
	if filter != "all" {
		active++
	}
	if platform != "" {
		active++
	}
	if sortKey != "recent" {
		active++
	}

	view := LibraryView{
		Games:       shown,
		Platforms:   platforms,
		Counts:      counts,
		Total:       len(games),
		Filter:      filter,
		Platform:    platform,
		Sort:        sortKey,
		ActiveCount: active,
	}
	render(w, r, http.StatusOK, Layout("Questlog — Library", r.URL.Path, "", LibraryPage(view)))
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	games, err := s.store.List(r.Context(), nil)
	if err != nil {
		serverError(w, err)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	view := SearchView{Q: q, Games: filterSearch(games, q)}
	render(w, r, http.StatusOK, Layout("Questlog — Search", r.URL.Path, q, SearchPage(view)))
}

func (s *Server) handleNewForm(w http.ResponseWriter, r *http.Request) {
	view := FormView{Game: &model.Game{Status: model.StatusWishlist}, IsEdit: false}
	render(w, r, http.StatusOK, Layout("Questlog — Add a game", r.URL.Path, "", GameFormPage(view)))
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	g := parseGameForm(r)
	if err := g.Validate(); err != nil {
		renderFormError(w, r, FormView{Game: g, IsEdit: false}, err.Error())
		return
	}
	created, err := s.store.Create(r.Context(), g)
	if err != nil {
		var dup *repo.ErrDuplicate
		if errors.As(err, &dup) {
			renderFormError(w, r, FormView{Game: g, IsEdit: false}, duplicateMessage(dup))
			return
		}
		serverError(w, err)
		return
	}
	http.Redirect(w, r, gamePath(created.ID), http.StatusSeeOther)
}

func (s *Server) handleDetail(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	g, err := s.store.Get(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	view := DetailView{Game: g, Related: relatedGames(r.Context(), s, g), Error: r.URL.Query().Get("error")}
	render(w, r, http.StatusOK, Layout("Questlog — "+g.Title, r.URL.Path, "", DetailPage(view)))
}

func (s *Server) handleEditForm(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	g, err := s.store.Get(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	view := FormView{Game: g, IsEdit: true}
	render(w, r, http.StatusOK, Layout("Questlog — Edit game", r.URL.Path, "", GameFormPage(view)))
}

func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	g := parseGameForm(r)
	if err := g.Validate(); err != nil {
		renderFormError(w, r, FormView{Game: g, IsEdit: true}, err.Error())
		return
	}
	updated, err := s.store.Update(r.Context(), id, g)
	if err != nil {
		var dup *repo.ErrDuplicate
		if errors.As(err, &dup) {
			g.ID = id
			renderFormError(w, r, FormView{Game: g, IsEdit: true}, duplicateMessage(dup))
			return
		}
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		serverError(w, err)
		return
	}
	http.Redirect(w, r, gamePath(updated.ID), http.StatusSeeOther)
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.Delete(r.Context(), id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		serverError(w, err)
		return
	}
	if r.Header.Get("HX-Request") != "" {
		w.Header().Set("HX-Redirect", "/")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleEnrich mirrors the old frontend's "Get cover online": it looks
// up the game in the catalog and fills missing metadata, keeping
// rating/status/notes.
func (s *Server) handleEnrich(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	g, err := s.store.Get(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}

	matches := s.catalog.Search(r.Context(), g.Title)
	lower := strings.ToLower(strings.TrimSpace(g.Title))
	var match *catalog.Result
	for i := range matches {
		if strings.ToLower(matches[i].Name) == lower {
			match = &matches[i]
			break
		}
	}
	if match == nil {
		for i := range matches {
			if matches[i].Source == "igdb" {
				match = &matches[i]
				break
			}
		}
	}
	if match == nil && len(matches) > 0 {
		match = &matches[0]
	}
	if match == nil {
		http.Redirect(w, r, gamePath(id)+"?error="+urlQuery(`No cover found for "`+g.Title+`".`), http.StatusSeeOther)
		return
	}

	details, err := s.catalog.Details(r.Context(), match.Source, match.AppID)
	if err != nil {
		http.Redirect(w, r, gamePath(id)+"?error="+urlQuery("Lookup failed: "+err.Error()), http.StatusSeeOther)
		return
	}

	update := *g
	if g.Platform == "" {
		update.Platform = details.Platform
	}
	if g.Year == nil {
		update.Year = details.Year
	}
	if g.Genre == "" {
		update.Genre = details.Genre
	}
	if g.CoverURL == "" {
		update.CoverURL = details.CoverURL
	}
	if g.Description == "" {
		update.Description = details.Description
	}
	if details.Source == "steam" {
		update.SteamAppID = &details.AppID
	} else {
		update.SteamAppID = nil
	}
	if details.TimeToBeatMinutes != nil {
		update.TimeToBeatMinutes = details.TimeToBeatMinutes
	}

	if _, err := s.store.Update(r.Context(), id, &update); err != nil {
		http.Redirect(w, r, gamePath(id)+"?error="+urlQuery("Update failed: "+err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, gamePath(id), http.StatusSeeOther)
}

// ---- HTMX partials ----

func (s *Server) handleCatalogSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.FormValue("title"))
	if len(q) < 2 {
		renderFragment(w, r, CatalogSuggestions(nil, false))
		return
	}
	results := s.catalog.Search(r.Context(), q)
	renderFragment(w, r, CatalogSuggestions(results, true))
}

func (s *Server) handleCatalogApp(w http.ResponseWriter, r *http.Request) {
	source := r.PathValue("source")
	appID, err := strconv.ParseInt(r.PathValue("appid"), 10, 64)
	if err != nil || appID <= 0 {
		http.Error(w, "invalid app id", http.StatusBadRequest)
		return
	}
	cur := parseGameForm(r)
	details, err := s.catalog.Details(r.Context(), source, appID)
	if err != nil {
		renderFragment(w, r, CatalogFillError(err.Error()))
		return
	}

	filled := &model.Game{
		Title:    details.Name,
		CoverURL: details.CoverURL,
		Status:   cur.Status,
	}
	if cur.Year != nil {
		filled.Year = cur.Year
	} else {
		filled.Year = details.Year
	}
	if cur.Genre != "" {
		filled.Genre = cur.Genre
	} else {
		filled.Genre = details.Genre
	}
	if cur.Platform != "" {
		filled.Platform = cur.Platform
	} else {
		filled.Platform = details.Platform
	}
	if cur.Description != "" {
		filled.Description = cur.Description
	} else {
		filled.Description = details.Description
	}
	if details.Source == "steam" {
		filled.SteamAppID = &details.AppID
	}
	if details.TimeToBeatMinutes != nil {
		filled.TimeToBeatMinutes = details.TimeToBeatMinutes
	} else {
		filled.TimeToBeatMinutes = cur.TimeToBeatMinutes
	}

	renderFragment(w, r, GameFields(FormView{Game: filled}))
}

// ---- Helpers ----

func gamePath(id int64) string {
	return "/games/" + strconv.FormatInt(id, 10)
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "invalid game id", http.StatusBadRequest)
		return 0, false
	}
	return id, true
}

func renderFormError(w http.ResponseWriter, r *http.Request, v FormView, msg string) {
	v.Error = msg
	title := "Questlog — Add a game"
	if v.IsEdit {
		title = "Questlog — Edit game"
	}
	render(w, r, http.StatusOK, Layout(title, r.URL.Path, "", GameFormPage(v)))
}

func serverError(w http.ResponseWriter, err error) {
	log.Printf("web: %v", err)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}

func duplicateMessage(dup *repo.ErrDuplicate) string {
	return strconv.Quote(dup.Existing.Title) + " is already in your collection as " +
		metaFor(dup.Existing.Status).Label + " — edit that card to change its list."
}

func validFilter(f string) bool {
	if f == "" || f == "all" {
		return true
	}
	return model.Status(f).Valid()
}

func validSort(s string) bool {
	return s == "" || s == "recent" || s == "title" || s == "rating"
}

// pickFeatured returns the hero card: the top-rated played game, else
// the most recently added game.
func pickFeatured(games []model.Game) *model.Game {
	var best *model.Game
	for i := range games {
		if games[i].Status == model.StatusPlayed && games[i].Rating > 0 {
			if best == nil || games[i].Rating > best.Rating ||
				(games[i].Rating == best.Rating && games[i].UpdatedAt.After(best.UpdatedAt)) {
				g := games[i]
				best = &g
			}
		}
	}
	if best != nil {
		return best
	}
	if len(games) > 0 {
		g := games[0]
		return &g
	}
	return nil
}

// buildRows groups games under each status in display order.
func buildRows(games []model.Game) []StatusRow {
	rows := make([]StatusRow, 0, len(model.AllStatuses))
	for _, st := range model.AllStatuses {
		var matches []model.Game
		for _, g := range games {
			if g.Status == st {
				matches = append(matches, g)
			}
		}
		rows = append(rows, StatusRow{Status: st, Games: matches})
	}
	return rows
}

// relatedGames returns up to 12 games sharing the same status, ranked
// by rating then recency.
func relatedGames(ctx context.Context, s *Server, g *model.Game) []model.Game {
	all, err := s.store.List(ctx, &g.Status)
	if err != nil {
		return nil
	}
	var related []model.Game
	for _, x := range all {
		if x.ID != g.ID {
			related = append(related, x)
		}
	}
	sort.SliceStable(related, func(i, j int) bool {
		if related[i].Rating != related[j].Rating {
			return related[i].Rating > related[j].Rating
		}
		return related[i].UpdatedAt.After(related[j].UpdatedAt)
	})
	if len(related) > 12 {
		related = related[:12]
	}
	return related
}

// platformSummary returns the distinct platforms (sorted) and their
// counts across the whole collection.
func platformSummary(games []model.Game) ([]string, map[string]int) {
	seen := map[string]bool{}
	counts := map[string]int{}
	for _, g := range games {
		p := strings.TrimSpace(g.Platform)
		if p == "" {
			continue
		}
		counts[p]++
		seen[p] = true
	}
	platforms := make([]string, 0, len(seen))
	for p := range seen {
		platforms = append(platforms, p)
	}
	sort.Strings(platforms)
	return platforms, counts
}

// filterSortGames applies the library filter/platform/sort.
func filterSortGames(games []model.Game, filter, platform, sortKey string) []model.Game {
	out := make([]model.Game, 0, len(games))
	for _, g := range games {
		if filter != "" && filter != "all" && g.Status != model.Status(filter) {
			continue
		}
		if platform != "" && strings.TrimSpace(g.Platform) != platform {
			continue
		}
		out = append(out, g)
	}
	switch sortKey {
	case "title":
		sort.SliceStable(out, func(i, j int) bool {
			return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title)
		})
	case "rating":
		sort.SliceStable(out, func(i, j int) bool {
			if out[i].Rating != out[j].Rating {
				return out[i].Rating > out[j].Rating
			}
			return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title)
		})
	default:
		sort.SliceStable(out, func(i, j int) bool {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		})
	}
	return out
}

// filterSearch matches a game against title/platform/genre/status.
func filterSearch(games []model.Game, q string) []model.Game {
	needle := strings.ToLower(q)
	if needle == "" {
		return games
	}
	var out []model.Game
	for _, g := range games {
		fields := []string{g.Title, g.Platform, g.Genre, string(g.Status)}
		for _, f := range fields {
			if strings.Contains(strings.ToLower(f), needle) {
				out = append(out, g)
				break
			}
		}
	}
	return out
}
