package igdb

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mockServer serves the Twitch token endpoint and the IGDB games
// endpoint; the games handler can branch on the query body.
func mockServer(t *testing.T, gamesHandler func(body string) (int, string)) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "grant_type=client_credentials") {
			http.Error(w, "bad token request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok123","expires_in":5400,"token_type":"bearer"}`))
	})
	mux.HandleFunc("/games", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get("Client-ID") != "cid" {
			http.Error(w, "bad client id", http.StatusUnauthorized)
			return
		}
		if r.Header.Get("Authorization") != "Bearer tok123" {
			http.Error(w, "bad auth", http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		code, out := gamesHandler(string(body))
		if code != http.StatusOK {
			http.Error(w, "error", code)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(out))
	})
	return httptest.NewServer(mux)
}

const searchJSON = `[
  {"id": 135307, "name": "The Legend of Zelda: Tears of the Kingdom",
   "cover": {"url": "//images.igdb.com/igdb/image/upload/t_cover_big/co4pnp.jpg"},
   "first_release_date": 1683907200,
   "platforms": [{"name": "Nintendo Switch"}]},
  {"id": 998, "name": "Zelda II: The Adventure of Link",
   "first_release_date": 536976000,
   "platforms": [{"name": "NES"}]}
]`

const detailJSON = `[
  {"id": 135307, "name": "The Legend of Zelda: Tears of the Kingdom",
   "summary": "An epic adventure across the skies and depths of Hyrule.",
   "cover": {"url": "//images.igdb.com/igdb/image/upload/t_cover_big/co4pnp.jpg"},
   "first_release_date": 1683907200,
   "platforms": [{"name": "Nintendo Switch"}],
   "genres": [{"name": "Action"}, {"name": "Adventure"}]}
]`

func TestCoverURLNormalizesAnySizeToken(t *testing.T) {
	cases := map[string]string{
		"": "",
		"//images.igdb.com/igdb/image/upload/t_thumb/co5vmg.jpg":
			"https://images.igdb.com/igdb/image/upload/t_cover_big_2x/co5vmg.jpg",
		"//images.igdb.com/igdb/image/upload/t_cover_big/co1p4m.jpg":
			"https://images.igdb.com/igdb/image/upload/t_cover_big_2x/co1p4m.jpg",
		"https://cdn.example.com/custom/art.png":
			"https://cdn.example.com/custom/art.png", // no size token: untouched
	}
	for in, want := range cases {
		if got := coverURL(in); got != want {
			t.Errorf("coverURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDisabledWithoutCredentials(t *testing.T) {
	c := New("", "secret", "", "")
	if c.Enabled() {
		t.Fatal("client without credentials should be disabled")
	}
	c2 := New("cid", "", "", "")
	if c2.Enabled() {
		t.Fatal("client without secret should be disabled")
	}
}

func TestSearch(t *testing.T) {
	srv := mockServer(t, func(body string) (int, string) {
		if !strings.Contains(body, `search "zelda"`) {
			return http.StatusBadRequest, `[]`
		}
		return http.StatusOK, searchJSON
	})
	defer srv.Close()

	c := New("cid", "secret", srv.URL, srv.URL+"/oauth2/token")
	if !c.Enabled() {
		t.Fatal("client with credentials should be enabled")
	}
	results, err := c.Search(context.Background(), "zelda", 6)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	r := results[0]
	if r.AppID != 135307 || r.Name != "The Legend of Zelda: Tears of the Kingdom" {
		t.Errorf("unexpected first result: %+v", r)
	}
	if r.Platform != "Nintendo Switch" {
		t.Errorf("expected platform Nintendo Switch, got %q", r.Platform)
	}
}

func TestAppDetails(t *testing.T) {
	srv := mockServer(t, func(body string) (int, string) {
		if !strings.Contains(body, "where id = 135307;") {
			return http.StatusBadRequest, `[]`
		}
		return http.StatusOK, detailJSON
	})
	defer srv.Close()

	c := New("cid", "secret", srv.URL, srv.URL+"/oauth2/token")
	d, err := c.AppDetails(context.Background(), 135307)
	if err != nil {
		t.Fatalf("appdetails: %v", err)
	}
	if d.Name != "The Legend of Zelda: Tears of the Kingdom" {
		t.Errorf("name: %q", d.Name)
	}
	if d.Year == nil || *d.Year != 2023 {
		t.Errorf("year: %v (first_release_date 1683907200 = 2023-05-12)", d.Year)
	}
	if d.Genre != "Action, Adventure" {
		t.Errorf("genre: %q", d.Genre)
	}
	if d.Platform != "Nintendo Switch" {
		t.Errorf("platform: %q", d.Platform)
	}
	// Cover must be https and 2x portrait, even when the API returns
	// a thumbnail-size token (t_thumb) or the big cover directly.
	if d.CoverURL != "https://images.igdb.com/igdb/image/upload/t_cover_big_2x/co4pnp.jpg" {
		t.Errorf("cover: %q", d.CoverURL)
	}
	if !strings.Contains(d.Description, "Hyrule") {
		t.Errorf("description: %q", d.Description)
	}
}

func TestTokenCaching(t *testing.T) {
	var tokenCalls int
	srv := mockServer(t, func(body string) (int, string) {
		return http.StatusOK, searchJSON
	})
	defer srv.Close()

	// Count token requests by wrapping the default mock handler.
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		tokenCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok123","expires_in":5400}`))
	})
	mux.HandleFunc("/games", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(searchJSON))
	})
	srv2 := httptest.NewServer(mux)
	defer srv2.Close()

	c := New("cid", "secret", srv2.URL, srv2.URL+"/oauth2/token")
	for i := 0; i < 3; i++ {
		if _, err := c.Search(context.Background(), "zelda", 6); err != nil {
			t.Fatalf("search %d: %v", i, err)
		}
	}
	if tokenCalls != 1 {
		t.Errorf("expected token fetched once (cached), got %d calls", tokenCalls)
	}
}
