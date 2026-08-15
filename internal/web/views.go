package web

import (
	"fmt"
	"net/url"

	"github.com/leoarkiteto/questlog-api/internal/model"
)

// StatusRow groups games under one status for the dashboard rows.
type StatusRow struct {
	Status model.Status
	Games  []model.Game
}

// DashboardView is the dashboard page model.
type DashboardView struct {
	Featured *model.Game
	Rows     []StatusRow
}

// LibraryView is the library page model (server-side filter/sort).
type LibraryView struct {
	Games       []model.Game
	Platforms   []string
	Counts      map[string]int
	Total       int
	Filter      string // status value or "all"
	Platform    string // selected platform, "" = all
	Sort        string // "recent" | "title" | "rating"
	ActiveCount int
}

// SearchView is the search-results page model.
type SearchView struct {
	Q     string
	Games []model.Game
}

// DetailView is the game-detail page model.
type DetailView struct {
	Game    *model.Game
	Related []model.Game
	Error   string
}

// FormView is the add/edit form page model. Game is nil for a blank
// "add" form; IsEdit distinguishes edit (id known) from new.
type FormView struct {
	Game   *model.Game
	Error  string
	IsEdit bool
}

// PlatformCount is one row in the library filter panel.
type PlatformCount struct {
	Name  string
	Count int
}

// featuredLabel reports whether the hero is "Top rated" or "Latest".
func featuredLabel(g *model.Game) string {
	if g.Status == model.StatusPlayed {
		return "Top rated"
	}
	return "Latest"
}

// featuredMeta joins platform + year for the hero caption.
func featuredMeta(g *model.Game) string {
	var parts []string
	if g.Platform != "" {
		parts = append(parts, g.Platform)
	}
	if g.Year != nil {
		parts = append(parts, fmt.Sprintf("%d", *g.Year))
	}
	return joinNonEmpty(parts, " · ")
}

// joinNonEmpty joins non-empty strings with sep.
func joinNonEmpty(parts []string, sep string) string {
	out := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		if out != "" {
			out += sep
		}
		out += p
	}
	return out
}

// libraryURL builds the /library URL for a filter/platform/sort combo,
// omitting defaults so the URL stays clean.
func libraryURL(filter, platform, sort string) string {
	q := url.Values{}
	if filter != "" && filter != "all" {
		q.Set("filter", filter)
	}
	if platform != "" {
		q.Set("platform", platform)
	}
	if sort != "" && sort != "recent" {
		q.Set("sort", sort)
	}
	if len(q) == 0 {
		return "/library"
	}
	return "/library?" + q.Encode()
}

// sortLabel returns the human label for a library sort key.
func sortLabel(sort string) string {
	switch sort {
	case "title":
		return "Title A–Z"
	case "rating":
		return "Rating (highest first)"
	default:
		return "Recent"
	}
}

// filterButtonClass styles the "Filter" button (highlighted when active).
func filterButtonClass(activeCount int) string {
	if activeCount > 0 {
		return "bg-red-600 text-white"
	}
	return "bg-zinc-900 text-zinc-400 ring-1 ring-white/10 hover:text-zinc-200"
}

// emptyTitle chooses the empty-state heading.
func emptyTitle(activeCount int) string {
	if activeCount == 0 {
		return "Your library is empty"
	}
	return "Nothing matches"
}

// filterOptionClass styles a filter-panel option.
func filterOptionClass(active bool) string {
	base := "flex w-full items-center justify-between gap-2 rounded-xl px-4 py-3 text-sm font-medium transition"
	if active {
		return base + " bg-red-600/15 text-red-300 ring-1 ring-red-500/30"
	}
	return base + " text-zinc-300 hover:bg-white/5"
}

// platformCount returns the count for a platform (0 if absent).
func platformCount(counts map[string]int, p string) int {
	return counts[p]
}

// resultCountLabel renders the search/grid count caption.
func resultCountLabel(q string, n int) string {
	if q == "" {
		return "All games"
	}
	if n == 1 {
		return "1 result"
	}
	return fmt.Sprintf("%d results", n)
}

// sourceLabel maps a catalog source key to a display label.
func sourceLabel(source string) string {
	switch source {
	case "steam":
		return "Steam"
	case "igdb":
		return "IGDB"
	}
	return source
}

// dashboardRowClass adds the separator + spacing between dashboard rows.
func dashboardRowClass(i int) string {
	if i > 0 {
		return "border-t border-white/10 pt-8"
	}
	return ""
}
