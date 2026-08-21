package web

import (
	"context"
	"crypto/subtle"
	"errors"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/leoarkiteto/questlog-api/internal/auth"
	"github.com/leoarkiteto/questlog-api/internal/catalog"
	"github.com/leoarkiteto/questlog-api/internal/model"
	"github.com/leoarkiteto/questlog-api/internal/repo"
)

// Server holds the dependencies for the HTML UI.
type Server struct {
	store   *repo.Store
	auth    *auth.Service
	catalog *catalog.Service
}

// New builds the HTTP handler for the whole site: HTML pages, HTMX
// partials, and the health check. Every route except /login, /health
// and /static/ requires a valid session.
func New(store *repo.Store, authService *auth.Service, catalogService *catalog.Service) http.Handler {
	s := &Server{store: store, auth: authService, catalog: catalogService}

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
	mux.HandleFunc("GET /login", s.handleLoginForm)
	mux.HandleFunc("POST /login", s.handleLogin)
	mux.HandleFunc("POST /logout", s.handleLogout)
	mux.HandleFunc("GET /profile", s.handleProfile)
	mux.HandleFunc("GET /health", s.handleHealth)

	return s.logMiddleware(s.requireAuth(s.csrfProtect(mux)))
}

// ---- Middleware ----

func (s *Server) logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		log.Printf("%s %s (%s)", r.Method, r.URL.Path, r.RemoteAddr)
	})
}

// requireAuth gates the whole app except a few public endpoints. On
// success it stores the resolved Session in the request context.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isPublicPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		sess := s.currentSession(r)
		if sess == nil {
			clearSessionCookie(w)
			redirectLogin(w, r)
			return
		}
		ctx := context.WithValue(r.Context(), sessionCtxKey{}, sess)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// isPublicPath reports whether a path is reachable without a session.
func isPublicPath(path string) bool {
	return path == "/login" || path == "/health" || strings.HasPrefix(path, "/static/")
}

// redirectLogin sends unauthenticated visitors to the sign-in page,
// preserving the destination so they land back where they were heading.
func redirectLogin(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Request") != "" {
		w.Header().Set("HX-Redirect", "/login")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
}

// csrfProtect requires a valid CSRF token on every state-changing
// request. Authenticated requests use the per-session token from the
// database (checked against the session in context); the login form,
// which has no session yet, uses the anonymous double-submit cookie set
// on GET /login.
func (s *Server) csrfProtect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		submitted := r.FormValue("_csrf")
		if submitted == "" {
			submitted = r.Header.Get("X-CSRF-Token")
		}
		if sess := sessionFrom(r); sess != nil {
			if !auth.ValidCSRF(sess, submitted) {
				http.Error(w, "invalid CSRF token", http.StatusForbidden)
				return
			}
		} else if !validAnonCSRF(r) {
			http.Error(w, "invalid CSRF token", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---- Session context ----

type sessionCtxKey struct{}

// sessionFrom returns the session stored by requireAuth, or nil.
func sessionFrom(r *http.Request) *auth.Session {
	sess, _ := r.Context().Value(sessionCtxKey{}).(*auth.Session)
	return sess
}

// userID returns the id of the signed-in user for the request. Handlers
// run behind requireAuth, so this is never 0 in practice.
func userID(r *http.Request) int64 {
	if sess := sessionFrom(r); sess != nil {
		return sess.User.ID
	}
	return 0
}

// currentSession resolves the session cookie, or nil when absent or
// invalid. Called by requireAuth and by /login to detect signed-in users.
func (s *Server) currentSession(r *http.Request) *auth.Session {
	c, err := r.Cookie(auth.SessionCookie)
	if err != nil {
		return nil
	}
	sess, err := s.auth.SessionByToken(r.Context(), c.Value)
	if err != nil {
		return nil
	}
	return sess
}

// sessionCookie builds the session cookie for a raw token.
func sessionCookie(r *http.Request, raw string) *http.Cookie {
	c := &http.Cookie{
		Name:     auth.SessionCookie,
		Value:    raw,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(auth.SessionMaxAge.Seconds()),
	}
	if secureRequest(r) {
		c.Secure = true
	}
	return c
}

// clearSessionCookie expires the session cookie (used on logout and
// when a session is invalid).
func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// secureRequest reports whether the request arrived over TLS (Render
// terminates TLS and forwards X-Forwarded-Proto).
func secureRequest(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// ---- Auth handlers ----

func (s *Server) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	if s.currentSession(r) != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	view := LoginView{CSRFToken: s.anonCSRF(w, r)}
	if q := r.URL.Query().Get("error"); q != "" {
		view.Error = q
	}
	if q := r.URL.Query().Get("email"); q != "" {
		view.Email = q
	}
	render(w, r, http.StatusOK, Layout("Questlog — Sign in", r.URL.Path, "", nil, LoginPage(view)))
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !validAnonCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")

	u, err := s.auth.Authenticate(r.Context(), email, password)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			view := LoginView{
				Email:     auth.NormalizeEmail(email),
				Error:     err.Error(),
				CSRFToken: s.anonCSRF(w, r),
			}
			render(w, r, http.StatusUnauthorized, Layout("Questlog — Sign in", r.URL.Path, "", nil, LoginPage(view)))
			return
		}
		serverError(w, err)
		return
	}

	raw, err := s.auth.CreateSession(r.Context(), u.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	http.SetCookie(w, sessionCookie(r, raw))
	http.Redirect(w, r, nextPath(r), http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(auth.SessionCookie); err == nil {
		_ = s.auth.DestroySession(r.Context(), c.Value)
	}
	clearSessionCookie(w)
	if r.Header.Get("HX-Request") != "" {
		w.Header().Set("HX-Redirect", "/login")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// handleProfile renders the signed-in user's profile: account info and
// per-status collection stats.
func (s *Server) handleProfile(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	counts, err := s.store.CountByStatus(r.Context(), sess.User.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	total := 0
	for _, n := range counts {
		total += n
	}
	view := ProfileView{
		User:      sess.User,
		Counts:    counts,
		Total:     total,
		CSRFToken: sess.CSRFToken,
	}
	render(w, r, http.StatusOK, Layout("Questlog — Profile", r.URL.Path, "", sess, ProfilePage(view)))
}

// anonCSRF returns the anonymous CSRF token for the login form,
// refreshing the cookie when it is absent.
func (s *Server) anonCSRF(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie(auth.AnonCSRFCookie); err == nil && c.Value != "" {
		return c.Value
	}
	token, err := auth.NewRandomToken()
	if err != nil {
		return ""
	}
	http.SetCookie(w, &http.Cookie{
		Name:     auth.AnonCSRFCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   15 * 60, // 15 minutes; the form is short-lived
		Secure:   secureRequest(r),
	})
	return token
}

// validAnonCSRF checks the login form's double-submit token: the cookie
// set on GET /login must match the _csrf form field.
func validAnonCSRF(r *http.Request) bool {
	c, err := r.Cookie(auth.AnonCSRFCookie)
	if err != nil || c.Value == "" {
		return false
	}
	submitted := r.FormValue("_csrf")
	return subtle.ConstantTimeCompare([]byte(c.Value), []byte(submitted)) == 1
}

// nextPath sanitizes the ?next= destination after login, allowing only
// local absolute paths to avoid open redirects.
func nextPath(r *http.Request) string {
	next := strings.TrimSpace(r.FormValue("next"))
	if next == "" {
		return "/"
	}
	u, err := url.Parse(next)
	if err != nil || u.IsAbs() || u.Host != "" || !strings.HasPrefix(next, "/") ||
		strings.HasPrefix(next, "//") {
		return "/"
	}
	return next
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
	games, err := s.store.List(r.Context(), userID(r), nil)
	if err != nil {
		serverError(w, err)
		return
	}
	view := DashboardView{
		Featured: pickFeatured(games),
		Rows:     buildRows(games),
	}
	render(w, r, http.StatusOK, Layout("Questlog — my games", r.URL.Path, "", sessionFrom(r), DashboardPage(view)))
}

// loadMoreHeader marks the library "load more" sentinel request (plain
// hx-get). It must be distinct from hx-boost navigations, which also
// send HX-Request but want the full document (body swap), not a fragment.
const loadMoreHeader = "X-Questlog-LoadMore"

// libraryPageSize is how many cards each library page renders.
const libraryPageSize = 24

func (s *Server) handleLibrary(w http.ResponseWriter, r *http.Request) {
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
	page := queryPage(q.Get("page"))

	var status *model.Status
	if filter != "all" {
		st := model.Status(filter)
		status = &st
	}

	uid := userID(r)
	ctx := r.Context()
	offset := (page - 1) * libraryPageSize

	// "Load more" sentinel: return only the next cards + a fresh sentinel.
	if r.Header.Get(loadMoreHeader) == "1" {
		games, hasMore, err := s.store.ListPage(ctx, uid, status, platform, sortKey, libraryPageSize, offset)
		if err != nil {
			serverError(w, err)
			return
		}
		renderFragment(w, r, LibraryCards(games, nextLibraryPage(page, hasMore), hasMore, filter, platform, sortKey))
		return
	}

	// "All platforms" count: matches the current Status filter, with no
	// platform constraint (what clicking "All platforms" would show).
	allPlatformsCount, err := s.store.CountFiltered(ctx, uid, status, "")
	if err != nil {
		serverError(w, err)
		return
	}
	filteredCount, err := s.store.CountFiltered(ctx, uid, status, platform)
	if err != nil {
		serverError(w, err)
		return
	}
	platforms, counts, err := s.store.PlatformCounts(ctx, uid, status)
	if err != nil {
		serverError(w, err)
		return
	}
	games, hasMore, err := s.store.ListPage(ctx, uid, status, platform, sortKey, libraryPageSize, offset)
	if err != nil {
		serverError(w, err)
		return
	}

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
		Games:             games,
		Platforms:         platforms,
		Counts:            counts,
		AllPlatformsCount: allPlatformsCount,
		FilteredCount:     filteredCount,
		Filter:            filter,
		Platform:          platform,
		Sort:              sortKey,
		ActiveCount:       active,
		HasMore:           hasMore,
		NextPage:          nextLibraryPage(page, hasMore),
	}
	render(w, r, http.StatusOK, Layout("Questlog — Library", r.URL.Path, "", sessionFrom(r), LibraryPage(view)))
}

// queryPage parses the ?page= param, defaulting to 1 and clamping to >= 1.
func queryPage(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 1 {
		return 1
	}
	return n
}

// nextLibraryPage returns the page to load next, or 0 when exhausted.
func nextLibraryPage(page int, hasMore bool) int {
	if !hasMore {
		return 0
	}
	return page + 1
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	games, err := s.store.List(r.Context(), userID(r), nil)
	if err != nil {
		serverError(w, err)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	view := SearchView{Q: q, Games: filterSearch(games, q)}
	render(w, r, http.StatusOK, Layout("Questlog — Search", r.URL.Path, q, sessionFrom(r), SearchPage(view)))
}

func (s *Server) handleNewForm(w http.ResponseWriter, r *http.Request) {
	view := FormView{Game: &model.Game{Status: model.StatusWishlist}, IsEdit: false, CSRFToken: sessionFrom(r).CSRFToken}
	render(w, r, http.StatusOK, Layout("Questlog — Add a game", r.URL.Path, "", sessionFrom(r), GameFormPage(view)))
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	g := parseGameForm(r)
	if err := g.Validate(); err != nil {
		renderFormError(w, r, FormView{Game: g, IsEdit: false, CSRFToken: sessionFrom(r).CSRFToken}, err.Error())
		return
	}
	created, err := s.store.Create(r.Context(), userID(r), g)
	if err != nil {
		var dup *repo.ErrDuplicate
		if errors.As(err, &dup) {
			renderFormError(w, r, FormView{Game: g, IsEdit: false, CSRFToken: sessionFrom(r).CSRFToken}, duplicateMessage(dup))
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
	g, err := s.store.Get(r.Context(), userID(r), id)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	view := DetailView{
		Game:      g,
		Related:   relatedGames(r.Context(), s, userID(r), g),
		Error:     r.URL.Query().Get("error"),
		CSRFToken: sessionFrom(r).CSRFToken,
		Enriched:  r.URL.Query().Get("enriched") == "1",
	}
	render(w, r, http.StatusOK, Layout("Questlog — "+g.Title, r.URL.Path, "", sessionFrom(r), DetailPage(view)))
}

func (s *Server) handleEditForm(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	g, err := s.store.Get(r.Context(), userID(r), id)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	view := FormView{Game: g, IsEdit: true, CSRFToken: sessionFrom(r).CSRFToken}
	render(w, r, http.StatusOK, Layout("Questlog — Edit game", r.URL.Path, "", sessionFrom(r), GameFormPage(view)))
}

func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	g := parseGameForm(r)
	if err := g.Validate(); err != nil {
		renderFormError(w, r, FormView{Game: g, IsEdit: true, CSRFToken: sessionFrom(r).CSRFToken}, err.Error())
		return
	}
	updated, err := s.store.Update(r.Context(), userID(r), id, g)
	if err != nil {
		var dup *repo.ErrDuplicate
		if errors.As(err, &dup) {
			g.ID = id
			renderFormError(w, r, FormView{Game: g, IsEdit: true, CSRFToken: sessionFrom(r).CSRFToken}, duplicateMessage(dup))
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
	if err := s.store.Delete(r.Context(), userID(r), id); err != nil {
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
// rating/status/notes. HTMX requests get a fragment swap of the button
// (failed → retryable "Failed" with the error inline); plain requests
// keep the old ?error= redirect. On success the page reloads so the
// refreshed cover/metadata render, with ?enriched=1 flagging the "Done"
// button state.
func (s *Server) handleEnrich(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	isHX := r.Header.Get("HX-Request") != ""
	g, err := s.store.Get(r.Context(), userID(r), id)
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
		enrichFailure(w, r, id, isHX, `No cover found for "`+g.Title+`".`)
		return
	}

	details, err := s.catalog.Details(r.Context(), match.Source, match.AppID)
	if err != nil {
		enrichFailure(w, r, id, isHX, "Lookup failed: "+err.Error())
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

	if _, err := s.store.Update(r.Context(), userID(r), id, &update); err != nil {
		enrichFailure(w, r, id, isHX, "Update failed: "+err.Error())
		return
	}
	if isHX {
		w.Header().Set("HX-Redirect", gamePath(id)+"?enriched=1")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, gamePath(id)+"?enriched=1", http.StatusSeeOther)
}

// enrichFailure reports an enrich error: for HTMX it swaps the button
// into its retryable "Failed" state with the reason shown inline; plain
// requests get the pre-HTMX ?error= redirect.
func enrichFailure(w http.ResponseWriter, r *http.Request, id int64, isHX bool, msg string) {
	if isHX {
		renderFragment(w, r, enrichForm(id, sessionFrom(r).CSRFToken, enrichStateFailed, msg))
		return
	}
	http.Redirect(w, r, gamePath(id)+"?error="+urlQuery(msg), http.StatusSeeOther)
}

// ---- HTMX partials ----

func (s *Server) handleCatalogSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.FormValue("title"))
	if len(q) < 2 {
		renderFragment(w, r, CatalogSuggestions(nil, false, nil))
		return
	}
	results := s.catalog.Search(r.Context(), q)
	existing, err := s.store.FindDuplicate(r.Context(), userID(r), 0, &model.Game{Title: q})
	if err != nil {
		serverError(w, err)
		return
	}
	renderFragment(w, r, CatalogSuggestions(results, true, existing))
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

	existing, dupErr := s.store.FindDuplicate(r.Context(), userID(r), 0, filled)
	if dupErr != nil {
		serverError(w, dupErr)
		return
	}
	renderFragment(w, r, GameFields(FormView{Game: filled, Duplicate: existing}))
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
	render(w, r, http.StatusOK, Layout(title, r.URL.Path, "", sessionFrom(r), GameFormPage(v)))
}

func serverError(w http.ResponseWriter, err error) {
	log.Printf("web: %v", err)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}

func duplicateMessage(dup *repo.ErrDuplicate) string {
	return duplicateGameMessage(&dup.Existing)
}

func duplicateGameMessage(existing *model.Game) string {
	return strconv.Quote(existing.Title) + " is already in your collection as " +
		metaFor(existing.Status).Label + " — edit that card to change its list."
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

// buildRows groups games under each status in display order, newest
// entry into that status first (status_changed_at, same as the library's
// "recent" sort). That way editing a game into a new status surfaces it
// at the front of its row; the hero and search keep their own orderings.
func buildRows(games []model.Game) []StatusRow {
	rows := make([]StatusRow, 0, len(model.AllStatuses))
	for _, st := range model.AllStatuses {
		var matches []model.Game
		for _, g := range games {
			if g.Status == st {
				matches = append(matches, g)
			}
		}
		sort.SliceStable(matches, func(i, j int) bool {
			if !matches[i].StatusChangedAt.Equal(matches[j].StatusChangedAt) {
				return matches[i].StatusChangedAt.After(matches[j].StatusChangedAt)
			}
			return matches[i].ID > matches[j].ID
		})
		rows = append(rows, StatusRow{Status: st, Games: matches})
	}
	return rows
}

// relatedGames returns up to 12 games of the same user sharing the same
// status, ranked by rating then recency.
func relatedGames(ctx context.Context, s *Server, uid int64, g *model.Game) []model.Game {
	all, err := s.store.List(ctx, uid, &g.Status)
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
