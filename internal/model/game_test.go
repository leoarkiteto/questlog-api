package model

import "testing"

func TestStatusValid(t *testing.T) {
	for _, s := range AllStatuses {
		if !s.Valid() {
			t.Errorf("expected %q to be valid", s)
		}
	}
	for _, s := range []Status{"", "owned", "finished", "PLAYING"} {
		if s.Valid() {
			t.Errorf("expected %q to be invalid", s)
		}
	}
}

func TestGameValidate(t *testing.T) {
	year := 2022
	base := Game{Title: "Elden Ring", Status: StatusPlayed, Rating: 5, Year: &year}

	if err := base.Validate(); err != nil {
		t.Fatalf("valid game rejected: %v", err)
	}

	cases := []struct {
		mut  func(*Game)
		name string
	}{
		{func(g *Game) { g.Title = "  " }, "empty title"},
		{func(g *Game) { g.Status = "owned" }, "bad status"},
		{func(g *Game) { g.Rating = 6 }, "rating too high"},
		{func(g *Game) { g.Rating = -1 }, "negative rating"},
		{func(g *Game) { g.Year = intPtr(1900) }, "year too old"},
	}
	for _, tc := range cases {
		g := base
		tc.mut(&g)
		if err := g.Validate(); err == nil {
			t.Errorf("%s: expected validation error, got nil", tc.name)
		}
	}
}

func intPtr(v int) *int { return &v }
