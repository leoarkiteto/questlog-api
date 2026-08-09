// Package igdb is an API layer
// to get data when is not available on Steam API
package igdb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	defaultBase     = "https://api.igdb.com/v4"
	defaultTokenURL = "https://id.twitch.tv/oauth2/token"
)

// Client talks to the IGDB games database (api.igdb.com), which needs
// Twitch app credentials (Client-ID + Client-Secret). It is the
// fallback catalog for games that aren't on Steam (Switch, PS5…).
type Client struct {
	tokenExpiry  time.Time
	http         *http.Client
	clientID     string
	clientSecret string
	base         string
	tokenURL     string
	token        string
	mu           sync.Mutex
}

// New returns an IGDB client. When either credential is empty the
// client is disabled (Enabled() == false). base/tokenURL override the
// API roots (used in tests).
func New(clientID, clientSecret, base, tokenURL string) *Client {
	if base == "" {
		base = defaultBase
	}
	if tokenURL == "" {
		tokenURL = defaultTokenURL
	}
	return &Client{
		http:         &http.Client{Timeout: 20 * time.Second},
		clientID:     clientID,
		clientSecret: clientSecret,
		base:         strings.TrimSuffix(base, "/"),
		tokenURL:     tokenURL,
	}
}

// Enabled reports whether credentials are configured.
func (c *Client) Enabled() bool { return c.clientID != "" && c.clientSecret != "" }

// SearchResult is one match from IGDB search.
type SearchResult struct {
	Name     string `json:"name"`
	Platform string `json:"platform"`
	AppID    int64  `json:"appid"`
}

// Search looks up games by name via IGDB's fuzzy search.
func (c *Client) Search(ctx context.Context, term string, limit int) ([]SearchResult, error) {
	term = strings.TrimSpace(term)
	if term == "" {
		return nil, fmt.Errorf("search term is required")
	}
	if limit <= 0 || limit > 20 {
		limit = 6
	}
	body := fmt.Sprintf(
		`search %s; fields name, cover.url, first_release_date, platforms.name; limit %d;`,
		quote(term), limit,
	)

	var raw []struct {
		Name      string `json:"name"`
		Platforms []struct {
			Name string `json:"name"`
		} `json:"platforms"`
		ID int64 `json:"id"`
	}
	if err := c.postGames(ctx, body, &raw); err != nil {
		return nil, err
	}

	results := make([]SearchResult, 0, len(raw))
	for _, r := range raw {
		if r.ID <= 0 || r.Name == "" {
			continue
		}
		res := SearchResult{AppID: r.ID, Name: r.Name}
		if len(r.Platforms) > 0 && r.Platforms[0].Name != "" {
			res.Platform = r.Platforms[0].Name
		}
		results = append(results, res)
	}
	return results, nil
}

// AppDetails is the enriched data we expose for one IGDB game.
type AppDetails struct {
	Year        *int   `json:"year"`
	Name        string `json:"name"`
	CoverURL    string `json:"coverUrl"`
	Genre       string `json:"genre"`
	Platform    string `json:"platform"`
	Description string `json:"description"`
	AppID       int64  `json:"appid"`
}

// AppDetails fetches one game's full record.
func (c *Client) AppDetails(ctx context.Context, appID int64) (*AppDetails, error) {
	body := fmt.Sprintf(
		`fields name, cover.url, first_release_date, genres.name, platforms.name, summary; where id = %d;`,
		appID,
	)

	var raw []struct {
		Name    string `json:"name"`
		Summary string `json:"summary"`
		Cover   struct {
			URL string `json:"url"`
		} `json:"cover"`
		Platforms []struct {
			Name string `json:"name"`
		} `json:"platforms"`
		Genres []struct {
			Name string `json:"name"`
		} `json:"genres"`
		ID               int64 `json:"id"`
		FirstReleaseDate int64 `json:"first_release_date"`
	}
	if err := c.postGames(ctx, body, &raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 || raw[0].ID == 0 || raw[0].Name == "" {
		return nil, fmt.Errorf("igdb returned no data for app %d", appID)
	}
	d := raw[0]

	details := &AppDetails{
		AppID:       d.ID,
		Name:        d.Name,
		CoverURL:    coverURL(d.Cover.URL),
		Year:        yearFromEpoch(d.FirstReleaseDate),
		Description: strings.TrimSpace(d.Summary),
	}
	if len(d.Platforms) > 0 {
		details.Platform = d.Platforms[0].Name
	}
	genres := make([]string, 0, len(d.Genres))
	for _, g := range d.Genres {
		if g.Name != "" {
			genres = append(genres, g.Name)
		}
	}
	details.Genre = strings.Join(genres, ", ")
	return details, nil
}

// postGames issues the IGDB v4 "games" endpoint query with the Twitch
// bearer token, retrying once if the token expired mid-flight.
func (c *Client) postGames(ctx context.Context, body string, dst any) error {
	token, err := c.bearerToken(ctx)
	if err != nil {
		return err
	}
	resp, err := c.post(ctx, body, token)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		// Token may have expired; clear and retry once.
		c.mu.Lock()
		c.token = ""
		c.tokenExpiry = time.Time{}
		c.mu.Unlock()

		token, err = c.bearerToken(ctx)
		if err != nil {
			return err
		}
		resp, err = c.post(ctx, body, token)
		if err != nil {
			return err
		}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("igdb responded %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("decode igdb response: %w", err)
	}
	return nil
}

func (c *Client) post(ctx context.Context, body, token string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.base+"/games",
		bytes.NewBufferString(body),
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Client-ID", c.clientID)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	return c.http.Do(req)
}

// bearerToken returns a cached Twitch app token, fetching a fresh one
// when missing or about to expire.
func (c *Client) bearerToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.tokenExpiry) {
		return c.token, nil
	}

	form := url.Values{
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
		"grant_type":    {"client_credentials"},
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.tokenURL,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("igdb token request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("igdb token endpoint responded %s", resp.Status)
	}

	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return "", fmt.Errorf("decode igdb token response: %w", err)
	}
	if tok.AccessToken == "" {
		return "", fmt.Errorf("igdb token response missing access_token")
	}
	// Refresh a bit early to avoid mid-call expiry.
	lifetime := max(tok.ExpiresIn-60, 60)
	c.token = tok.AccessToken
	c.tokenExpiry = time.Now().Add(time.Duration(lifetime) * time.Second)
	return c.token, nil
}

// sizeRe matches IGDB's image size tokens (/t_thumb/, /t_cover_big/, …)
// so we can request the portrait 2x box art regardless of what the API
// returned.
var sizeRe = regexp.MustCompile(`/t_[a-z0-9_]+/`)

// coverURL converts an IGDB cover URL into an https portrait 2x URL
// that fits our 2:3 cards. Absolute URLs (e.g. test mocks) are kept.
func coverURL(raw string) string {
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return sizeRe.ReplaceAllString(raw, "/t_cover_big_2x/")
	}
	raw = strings.TrimPrefix(raw, "//")
	return "https://" + sizeRe.ReplaceAllString(raw, "/t_cover_big_2x/")
}

func yearFromEpoch(epochSec int64) *int {
	if epochSec <= 0 {
		return nil
	}
	y := time.Unix(epochSec, 0).UTC().Year()
	return &y
}

// quote wraps an IGDB query string in double quotes, escaping inner quotes.
func quote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}
