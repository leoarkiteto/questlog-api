package hltb

import (
	"encoding/json"
	"testing"
)

func TestBestMatchExactTitleWins(t *testing.T) {
	entries := []entry{
		{ID: 145312, Name: "Atomic Heart - Trapped in Limbo", Type: "dlc", MainSeconds: 11442},
		{ID: 5304, Name: "Limbo", Type: "game", MainSeconds: 12875},
		{ID: 111887, Name: "Kulebra and the Souls of Limbo", Type: "game", MainSeconds: 37730},
	}
	got := bestMatch("limbo", entries)
	if got == nil || got.ID != 5304 {
		t.Fatalf("exact match should win, got %+v", got)
	}
}

func TestBestMatchPrefersGameOverDLC(t *testing.T) {
	entries := []entry{
		{ID: 1, Name: "Witcher 3", Type: "dlc", MainSeconds: 300},
		{ID: 2, Name: "Witcher 3 Wild Hunt", Type: "game", MainSeconds: 18000},
	}
	got := bestMatch("the witcher 3", entries)
	if got == nil || got.ID != 2 {
		t.Fatalf("plain game should win over dlc, got %+v", got)
	}
}

func TestBestMatchEmpty(t *testing.T) {
	if got := bestMatch("anything", nil); got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestParseResponseFixture(t *testing.T) {
	// Shape captured from the real /api/bleed response for "Limbo".
	body := `{
	  "count": 15,
	  "data": [
	    {"game_id": 5304, "game_name": "Limbo", "game_alias": "", "game_type": "game",
	     "release_world": 2010, "comp_main": 12875, "comp_main_count": 3497,
	     "comp_plus": 15051, "comp_100": 24245, "count_comp": 18172,
	     "profile_platform": "PC, PlayStation 3"},
	    {"game_id": 145312, "game_name": "Atomic Heart - Trapped in Limbo", "game_alias": "",
	     "game_type": "dlc", "release_world": 2024, "comp_main": 11442, "comp_main_count": 25,
	     "comp_plus": 15410, "comp_100": 23789, "count_comp": 97}
	  ]
	}`
	var res struct {
		Count int     `json:"count"`
		Data  []entry `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	if res.Count != 15 || len(res.Data) != 2 {
		t.Fatalf("unexpected parse: %+v", res)
	}
	e := bestMatch("Limbo", res.Data)
	if e == nil || e.MainSeconds != 12875 {
		t.Fatalf("expected Limbo entry with 12875s, got %+v", e)
	}
	// 12875s ≈ 215 minutes (rounds to the nearest minute).
	if m := int(float64(e.MainSeconds)/60 + 0.5); m != 215 {
		t.Fatalf("expected ~215 minutes, got %d", m)
	}
}

func TestBestMatchNoData(t *testing.T) {
	entries := []entry{{ID: 9, Name: "Vaporware", Type: "game", MainSeconds: 0}}
	if got := bestMatch("vaporware", entries); got == nil {
		t.Fatal("match should be found even without playtime data")
	}
}
