// Package web renders the Questlog HTML UI (GOTTH: Go + Templ +
// Tailwind + HTMX). It replaces the old separate Next.js frontend.
package web

import (
	"fmt"
	"strings"

	"github.com/leoarkiteto/questlog-api/internal/model"
)

// StatusMeta holds the presentation data for one collection status,
// mirroring the STATUSES table from the old React frontend.
type StatusMeta struct {
	Status model.Status
	Label  string
	Hint   string
	Accent string // Tailwind text color class
	Badge  string // Tailwind badge classes
	Icon   string // icon key (see icons.go)
}

var statusMeta = map[model.Status]StatusMeta{
	model.StatusWishlist: {
		Status: model.StatusWishlist,
		Label:  "Wishlist",
		Hint:   "Games I want",
		Accent: "text-amber-400",
		Badge:  "bg-amber-400/10 text-amber-300 ring-amber-400/30",
		Icon:   "sparkles",
	},
	model.StatusPurchased: {
		Status: model.StatusPurchased,
		Label:  "Purchased",
		Hint:   "Bought, not played yet",
		Accent: "text-violet-400",
		Badge:  "bg-violet-400/10 text-violet-300 ring-violet-400/30",
		Icon:   "shopping-bag",
	},
	model.StatusPlaying: {
		Status: model.StatusPlaying,
		Label:  "Currently Playing",
		Hint:   "Games I'm on now",
		Accent: "text-sky-400",
		Badge:  "bg-sky-400/10 text-sky-300 ring-sky-400/30",
		Icon:   "gamepad-2",
	},
	model.StatusPlayed: {
		Status: model.StatusPlayed,
		Label:  "Played",
		Hint:   "Games I finished",
		Accent: "text-emerald-400",
		Badge:  "bg-emerald-400/10 text-emerald-300 ring-emerald-400/30",
		Icon:   "trophy",
	},
	model.StatusDropped: {
		Status: model.StatusDropped,
		Label:  "Dropped",
		Hint:   "Tried, didn't finish",
		Accent: "text-rose-400",
		Badge:  "bg-rose-400/10 text-rose-300 ring-rose-400/30",
		Icon:   "flag",
	},
}

// metaFor returns the presentation data for a status, defaulting to
// wishlist for unknown values.
func metaFor(s model.Status) StatusMeta {
	if m, ok := statusMeta[s]; ok {
		return m
	}
	return statusMeta[model.StatusWishlist]
}

// AllStatuses lists every status in display order.
func AllStatuses() []model.Status { return model.AllStatuses }

// Platforms are the choices offered in the add/edit form dropdown,
// matching the old frontend's PLATFORMS list.
var Platforms = []string{
	"Nintendo Switch",
	"Nintendo Switch 2",
	"PlayStation 5",
	"PlayStation 4",
	"Xbox Series",
	"Xbox One",
	"PlayStation 3",
	"PC",
	"Mobile",
}

// formatTimeToBeat renders "~3h 35m" from a minute count (HowLongToBeat
// main-story average).
func formatTimeToBeat(minutes int) string {
	if minutes < 60 {
		return fmt.Sprintf("~%dm", minutes)
	}
	h := minutes / 60
	m := minutes % 60
	if m == 0 {
		return fmt.Sprintf("~%dh", h)
	}
	return fmt.Sprintf("~%dh %dm", h, m)
}

// canRate reports whether a status shows a star rating (played/dropped).
func canRate(s model.Status) bool {
	return s == model.StatusPlayed || s == model.StatusDropped
}

// starClass returns the Tailwind classes for a rating star.
func starClass(size string, on bool) string {
	s := "h-5 w-5"
	switch size {
	case "sm":
		s = "h-4 w-4"
	case "lg":
		s = "h-8 w-8"
	}
	if on {
		return s + " fill-amber-400 text-amber-400 drop-shadow-[0_0_6px_rgba(251,191,36,0.45)]"
	}
	return s + " fill-zinc-700 text-zinc-700"
}

// coverInitial returns the fallback letter shown when a game has no
// cover art.
func coverInitial(title string) string {
	for _, r := range strings.TrimSpace(title) {
		return strings.ToUpper(string(r))
	}
	return "?"
}

// strDefault returns s, or dflt when s is empty.
func strDefault(s, dflt string) string {
	if s == "" {
		return dflt
	}
	return s
}

// navIconClass returns the stroke/color classes for a bottom-nav icon.
func navIconClass(active bool) string {
	if active {
		return "h-6 w-6 fill-none stroke-current text-red-500"
	}
	return "h-6 w-6 fill-none stroke-current text-zinc-400"
}

// ariaCurrent returns "page" for the active nav link, "" otherwise.
func ariaCurrent(active bool) string {
	if active {
		return "page"
	}
	return ""
}

// isLibrary reports whether path is within the library section.
func isLibrary(path string) bool { return strings.HasPrefix(path, "/library") }

// isProfile reports whether path is the profile page.
func isProfile(path string) bool { return path == "/profile" }

// coverClasses returns the Tailwind classes for a game card's cover.
func coverClasses(glow bool) string {
	if glow {
		return "aspect-[2/3] w-full rounded-lg ring-2 ring-orange-400/80 shadow-[0_0_18px_rgba(251,146,60,0.45)] transition duration-200 group-hover:ring-orange-300 group-hover:shadow-[0_0_24px_rgba(253,186,116,0.65)]"
	}
	return "aspect-[2/3] w-full rounded-lg shadow-lg ring-1 ring-white/10 transition duration-200 group-hover:ring-red-500/60 group-hover:shadow-red-500/10"
}

// starIconClass returns the Tailwind classes for the compact card star.
func starIconClass(on bool) string {
	if on {
		return "h-4 w-4 fill-amber-400 text-amber-400 drop-shadow-[0_0_6px_rgba(251,191,36,0.45)]"
	}
	return "h-4 w-4 fill-zinc-700 text-zinc-700"
}

// starTextClass returns the text color for the compact card rating.
func starTextClass(on bool) string {
	if on {
		return "text-amber-300/90"
	}
	return "text-zinc-600"
}

// platformBrand maps a free-text platform to one of the four supported
// brand icon families, matching the old frontend's platform.ts.
type platformBrand int

const (
	brandOther platformBrand = iota
	brandPlayStation
	brandXbox
	brandNintendo
	brandSteam
)

func brandOf(platform string) platformBrand {
	p := strings.ToLower(platform)
	switch {
	case strings.Contains(p, "playstation") || strings.Contains(p, "ps3") ||
		strings.Contains(p, "ps4") || strings.Contains(p, "ps5") ||
		strings.HasPrefix(p, "ps ") || p == "ps" || strings.Contains(p, "psp") ||
		strings.Contains(p, "ps vita") || strings.Contains(p, "psvita") ||
		strings.Contains(p, "psx"):
		return brandPlayStation
	case strings.Contains(p, "xbox"):
		return brandXbox
	case strings.Contains(p, "nintendo") || strings.Contains(p, "switch") ||
		strings.Contains(p, "wii") || strings.Contains(p, "3ds") ||
		strings.Contains(p, "2ds") || strings.Contains(p, "dsi") ||
		strings.Contains(p, "gamecube") || strings.Contains(p, "game boy") ||
		strings.Contains(p, "gameboy") || strings.Contains(p, "nes") ||
		strings.Contains(p, "snes") || strings.Contains(p, "n64"):
		return brandNintendo
	case strings.Contains(p, "pc") || strings.Contains(p, "steam") ||
		strings.Contains(p, "windows") || strings.Contains(p, " win") ||
		strings.Contains(p, "mac") || strings.Contains(p, "linux"):
		return brandSteam
	}
	return brandOther
}
