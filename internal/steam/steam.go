package steam

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Client talks to Steam's public endpoints. The Web API key is only
// needed for key-gated calls (e.g. owned-games import); search and
// app details are public but we still proxy them through the backend
// so the browser never talks to Steam directly.
type Client struct {
	http   *http.Client
	apiKey string
}

func New(apiKey string) *Client {
	return &Client{
		http: &http.Client{Timeout: 15 * time.Second},
		// Used if we ever call key-gated ISteam* endpoints.
		apiKey: apiKey,
	}
}

// SearchResult is one match from Steam's app search.
type SearchResult struct {
	AppID int64  `json:"appid"`
	Name  string `json:"name"`
}

// Search looks up apps by name via Steam's search suggestions endpoint.
func (c *Client) Search(ctx context.Context, term string) ([]SearchResult, error) {
	term = strings.TrimSpace(term)
	if term == "" {
		return nil, fmt.Errorf("search term is required")
	}
	u := "https://steamcommunity.com/actions/SearchApps/" + url.PathEscape(term)
	body, err := c.get(ctx, u)
	if err != nil {
		return nil, err
	}

	var raw []struct {
		AppID string `json:"appid"`
		Name  string `json:"name"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode steam search: %w", err)
	}

	results := make([]SearchResult, 0, len(raw))
	for _, r := range raw {
		id, err := strconv.ParseInt(r.AppID, 10, 64)
		if err != nil || id <= 0 {
			continue
		}
		results = append(results, SearchResult{AppID: id, Name: r.Name})
	}

	// Exact name match first, then shorter names (closest title).
	lower := strings.ToLower(term)
	sort.SliceStable(results, func(i, j int) bool {
		a, b := strings.ToLower(results[i].Name), strings.ToLower(results[j].Name)
		if a == lower {
			return true
		}
		if b == lower {
			return false
		}
		return len(results[i].Name) < len(results[j].Name)
	})
	if len(results) > 10 {
		results = results[:10]
	}
	return results, nil
}

// AppDetails is the enriched data we expose for one Steam app.
type AppDetails struct {
	AppID      int64    `json:"appid"`
	Name       string   `json:"name"`
	CoverURL   string   `json:"coverUrl"`
	Year       *int     `json:"year"`
	Genre      string   `json:"genre"`
	Platform   string   `json:"platform"`
	Description string  `json:"description"`
	Developers []string `json:"developers"`
	Metacritic *int     `json:"metacritic"`
}

// AppDetails fetches the Steam store page for an app and maps it to
// the fields Questlog cares about.
func (c *Client) AppDetails(ctx context.Context, appID int64) (*AppDetails, error) {
	u := fmt.Sprintf("https://store.steampowered.com/api/appdetails?appids=%d&l=en", appID)
	body, err := c.get(ctx, u)
	if err != nil {
		return nil, err
	}

	var raw map[string]struct {
		Success bool `json:"success"`
		Data    struct {
			Name            string `json:"name"`
			SteamAppID      int64  `json:"steam_appid"`
			HeaderImage     string `json:"header_image"`
			ShortDescription string `json:"short_description"`
			ReleaseDate     struct {
				Date string `json:"date"`
			} `json:"release_date"`
			Platforms struct {
				Windows bool `json:"windows"`
				Mac     bool `json:"mac"`
				Linux   bool `json:"linux"`
			} `json:"platforms"`
			Genres []struct {
				Description string `json:"description"`
			} `json:"genres"`
			Developers []string `json:"developers"`
			Metacritic struct {
				Score int `json:"score"`
			} `json:"metacritic"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode steam appdetails: %w", err)
	}

	entry, ok := raw[strconv.FormatInt(appID, 10)]
	if !ok || !entry.Success || entry.Data.SteamAppID == 0 {
		return nil, fmt.Errorf("steam returned no data for app %d", appID)
	}
	d := entry.Data

	details := &AppDetails{
		AppID:       d.SteamAppID,
		Name:        d.Name,
		CoverURL:    coverFor(appID, d.HeaderImage),
		Year:        parseYear(d.ReleaseDate.Date),
		Description: html.UnescapeString(strings.TrimSpace(d.ShortDescription)),
		Developers:  d.Developers,
	}
	if d.Platforms.Windows || d.Platforms.Mac || d.Platforms.Linux {
		details.Platform = "PC"
	}
	genres := make([]string, 0, len(d.Genres))
	for _, g := range d.Genres {
		if g.Description != "" {
			genres = append(genres, g.Description)
		}
	}
	details.Genre = strings.Join(genres, ", ")
	if d.Metacritic.Score > 0 {
		score := d.Metacritic.Score
		details.Metacritic = &score
	}
	return details, nil
}

// coverFor returns the portrait library art, which matches our 2:3
// card aspect ratio. library_600x900.jpg exists for essentially every
// published Steam app.
func coverFor(appID int64, _ string) string {
	return fmt.Sprintf(
		"https://shared.akamai.steamstatic.com/store_item_assets/steam/apps/%d/library_600x900.jpg",
		appID,
	)
}

var yearRe = regexp.MustCompile(`\b(19[5-9]\d|20\d{2})\b`)

func parseYear(date string) *int {
	if m := yearRe.FindStringSubmatch(date); len(m) == 2 {
		if y, err := strconv.Atoi(m[1]); err == nil {
			return &y
		}
	}
	return nil
}

func (c *Client) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	// Steam's CDN sometimes rejects requests without a browser-like UA.
	req.Header.Set("User-Agent", "Mozilla/5.0 (Questlog/1.0)")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("steam request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("steam responded %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	return body, nil
}
