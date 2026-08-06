package repo

import (
	"strings"
	"testing"

	"questlog/internal/model"
)

func TestNormalizeTitle(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Hollow Knight", "hollow knight"},
		{"hollow knight", "hollow knight"},
		{"  Hollow   Knight  ", "hollow knight"},
		{"\tHollow\nKnight\t", "hollow knight"},
		{"The Witcher 3: Wild Hunt", "the witcher 3: wild hunt"},
		{"", ""},
		{"   ", ""},
		{"Celeste", "celeste"},
	}
	for _, c := range cases {
		if got := normalizeTitle(c.in); got != c.want {
			t.Errorf("normalizeTitle(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestErrDuplicateError(t *testing.T) {
	g := model.Game{ID: 7, Title: "Hollow Knight", Status: model.StatusPlayed}
	err := &ErrDuplicate{Existing: g}
	if !strings.Contains(err.Error(), "Hollow Knight") {
		t.Errorf("ErrDuplicate.Error() = %q, want it to mention the title", err.Error())
	}
}
