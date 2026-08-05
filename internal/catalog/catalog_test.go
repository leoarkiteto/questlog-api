package catalog

import "testing"

func TestMergeResultsExactMatchFirst(t *testing.T) {
	steam := []Result{
		{Source: "steam", AppID: 100, Name: "Zelda II: The Adventure of Link"},
		{Source: "steam", AppID: 200, Name: "The Legend of Zelda"},
	}
	rawg := []Result{
		{Source: "rawg", AppID: 3328, Name: "The Legend of Zelda: Tears of the Kingdom", Platform: "Nintendo Switch"},
	}

	got := mergeResults(steam, rawg, "the legend of zelda")
	if len(got) != 3 {
		t.Fatalf("expected 3 results, got %d", len(got))
	}
	// Exact (case-insensitive) match floats to the top.
	if got[0].Name != "The Legend of Zelda" || got[0].Source != "steam" {
		t.Errorf("exact match should be first, got %+v", got[0])
	}
	// RAWG entry keeps its platform hint.
	if got[2].Platform != "Nintendo Switch" {
		t.Errorf("rawg platform hint lost: %+v", got[2])
	}
}

func TestMergeResultsCap(t *testing.T) {
	steam := make([]Result, 0, 10)
	for i := 1; i <= 10; i++ {
		steam = append(steam, Result{Source: "steam", AppID: int64(i), Name: "Game"})
	}
	rawg := make([]Result, 0, 10)
	for i := 1; i <= 10; i++ {
		rawg = append(rawg, Result{Source: "rawg", AppID: int64(i), Name: "Other"})
	}
	got := mergeResults(steam, rawg, "x")
	if len(got) != 12 {
		t.Fatalf("expected cap at 12, got %d", len(got))
	}
}

func TestMergeResultsEmpty(t *testing.T) {
	if got := mergeResults(nil, nil, "nothing"); len(got) != 0 {
		t.Fatalf("expected empty, got %d", len(got))
	}
}
