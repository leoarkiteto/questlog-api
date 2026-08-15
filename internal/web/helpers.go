package web

import (
	"fmt"
	"strconv"
)

// fieldLabel is the shared label class used across the form.
const fieldLabel = "mb-1.5 block text-xs font-semibold uppercase tracking-wider text-zinc-400"

// formAction returns the form's POST target.
func formAction(v FormView) string {
	if v.IsEdit {
		return fmt.Sprintf("/games/%d", v.Game.ID)
	}
	return "/games"
}

// submitLabel returns the submit button text.
func submitLabel(v FormView) string {
	if v.IsEdit {
		return "Save changes"
	}
	return "Add game"
}

// yearValue renders a *int year as a string (empty for nil).
func yearValue(year *int) string {
	if year == nil {
		return ""
	}
	return strconv.Itoa(*year)
}

// steamAppIDValue renders a *int64 as a string (empty for nil).
func steamAppIDValue(id *int64) string {
	if id == nil {
		return ""
	}
	return strconv.FormatInt(*id, 10)
}

// timeToBeatValue renders a *int as a string (empty for nil).
func timeToBeatValue(minutes *int) string {
	if minutes == nil {
		return ""
	}
	return strconv.Itoa(*minutes)
}

// platformInList reports whether p is one of the canonical platforms.
func platformInList(p string) bool {
	for _, x := range Platforms {
		if x == p {
			return true
		}
	}
	return false
}

// sourceBadgeClass colors a catalog suggestion's source badge.
func sourceBadgeClass(source string) string {
	if source == "steam" {
		return "bg-sky-500/15 text-sky-300"
	}
	return "bg-emerald-500/15 text-emerald-300"
}

// ratingInputClass styles an interactive rating star.
func ratingInputClass(on bool) string {
	if on {
		return "h-8 w-8 cursor-pointer fill-amber-400 text-amber-400 drop-shadow-[0_0_6px_rgba(251,191,36,0.45)]"
	}
	return "h-8 w-8 cursor-pointer fill-zinc-700 text-zinc-700"
}

// ratingHint renders the rating helper text.
func ratingHint(rating int) string {
	if rating == 0 {
		return "Tap a star"
	}
	return fmt.Sprintf("%d / 5", rating)
}
