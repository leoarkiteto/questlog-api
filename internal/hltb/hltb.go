// Package hltb queries HowLongToBeat for community "time to beat"
// data (how long the average player takes to finish a game's main
// story). Steam's own API has no such data.
//
// HLTB's search API needs a short token dance: fetching the homepage
// sets an anti-bot cookie, then GET /api/bleed/init returns a token
// plus a signed key/value pair that must ride along with every search
// (in the JSON body AND as x-* headers). Tokens expire; on a 403 we
// re-init and retry once.
package hltb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/cookiejar"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	base = "https://howlongtobeat.com"
	// HLTB's Cloudflare setup rejects requests without a real browser
	// user agent, and the init token is bound to the UA we send.
	ua = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
)

// Client talks to HowLongToBeat's search API. It needs no API key.
type Client struct {
	http *http.Client

	mu     sync.Mutex
	token  string // signed search token from /api/bleed/init
	hpKey  string // signed anti-bot key, sent as x-hp-key header + body field
	hpVal  string // signed anti-bot value, sent as x-hp-val header + body field
	warmed bool   // homepage cookies have been fetched
}

// New returns a client. The first search warms the session (homepage
// cookies + init token); afterwards the token is reused until a 403
// forces a refresh.
func New() *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{http: &http.Client{Timeout: 15 * time.Second, Jar: jar}}
}

// Result is the "how long to beat" data for one game.
type Result struct {
	Name              string
	TimeToBeatMinutes *int // main-story completion time, nil when unknown
}

// Search looks up a game by title and returns the community main-story
// completion time. A best-effort match is made: exact title first,
// then the first plain "game" entry (over DLC/spinoffs), then the top
// result. A non-match or missing data yields a nil TimeToBeatMinutes.
func (c *Client) Search(ctx context.Context, title string) (*Result, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, fmt.Errorf("search term is required")
	}
	body, err := c.postSearch(ctx, strings.Fields(title))
	if err != nil {
		return nil, err
	}

	var res struct {
		Count int     `json:"count"`
		Data  []entry `json:"data"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("decode hltb response: %w", err)
	}
	e := bestMatch(title, res.Data)
	if e == nil || e.MainSeconds <= 0 {
		return &Result{Name: title}, nil
	}
	minutes := int(math.Round(float64(e.MainSeconds) / 60))
	return &Result{Name: e.Name, TimeToBeatMinutes: &minutes}, nil
}

// entry is one game row in HLTB's search response.
type entry struct {
	ID          int64  `json:"game_id"`
	Name        string `json:"game_name"`
	Type        string `json:"game_type"` // "game" | "dlc" | ...
	MainSeconds int64  `json:"comp_main"`
}

// bestMatch ranks HLTB hits for a query: exact title first, then the
// first plain game (avoiding DLC packs that share the title), then the
// first hit the API returned (sorted by popularity).
func bestMatch(title string, entries []entry) *entry {
	lower := strings.ToLower(strings.TrimSpace(title))
	for i := range entries {
		if strings.ToLower(entries[i].Name) == lower {
			return &entries[i]
		}
	}
	for i := range entries {
		if entries[i].Type == "game" {
			return &entries[i]
		}
	}
	if len(entries) > 0 {
		return &entries[0]
	}
	return nil
}

// postSearch runs the signed search request, refreshing the session
// once if the token has expired (403).
func (c *Client) postSearch(ctx context.Context, terms []string) ([]byte, error) {
	resp, body, err := c.doSearch(ctx, terms)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusForbidden {
		// Token expired — re-init and retry exactly once.
		c.mu.Lock()
		c.token, c.hpKey, c.hpVal = "", "", ""
		c.mu.Unlock()
		if err := c.ensureReady(ctx); err != nil {
			return nil, err
		}
		resp, body, err = c.doSearch(ctx, terms)
		if err != nil {
			return nil, err
		}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hltb responded %s", resp.Status)
	}
	return body, nil
}

func (c *Client) doSearch(ctx context.Context, terms []string) (*http.Response, []byte, error) {
	if err := c.ensureReady(ctx); err != nil {
		return nil, nil, err
	}
	c.mu.Lock()
	token, hpKey, hpVal := c.token, c.hpKey, c.hpVal
	c.mu.Unlock()

	payload := map[string]any{
		"searchType":  "games",
		"searchTerms": terms,
		"searchPage":  1,
		"size":        8,
		"searchOptions": map[string]any{
			"games": map[string]any{
				"userId": 0, "platform": "", "sortCategory": "popular",
				"rangeCategory": "main",
				"rangeTime":     map[string]int{"min": 0, "max": 0},
				"gameplay":      map[string]string{"perspective": "", "flow": "", "genre": "", "difficulty": ""},
				"rangeYear":     map[string]int{"min": 0, "max": 0},
				"modifier":      "",
			},
			"users":       map[string]string{"sortCategory": "most"},
			"lists":       map[string]string{"sortCategory": "most"},
			"filter":      "",
			"sort":        0,
			"randomizer":  0,
		},
		"useCache": false,
	}
	payload[hpKey] = hpVal // signed field, required by the API

	reqBody, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/bleed", bytes.NewReader(reqBody))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Origin", base)
	req.Header.Set("Referer", base+"/")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-auth-token", token)
	req.Header.Set("x-hp-key", hpKey)
	req.Header.Set("x-hp-val", hpVal)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("hltb search request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, nil, err
	}
	return resp, body, nil
}

// ensureReady makes sure the session is warmed: homepage cookies
// fetched (sets the anti-bot cookie) and an init token obtained.
func (c *Client) ensureReady(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.warmed {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/", nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", ua)
		if resp, err := c.http.Do(req); err != nil {
			return fmt.Errorf("hltb session warmup failed: %w", err)
		} else {
			resp.Body.Close()
		}
		c.warmed = true
	}
	if c.token != "" {
		return nil
	}

	u := base + "/api/bleed/init?t=" + strconv.FormatInt(time.Now().UnixMilli(), 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Origin", base)
	req.Header.Set("Referer", base+"/")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("hltb init request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("hltb init responded %s", resp.Status)
	}
	var tok struct {
		Token string `json:"token"`
		Key   string `json:"hpKey"`
		Val   string `json:"hpVal"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return fmt.Errorf("decode hltb init response: %w", err)
	}
	if tok.Token == "" || tok.Key == "" || tok.Val == "" {
		return fmt.Errorf("hltb init response missing token or hp fields")
	}
	c.token, c.hpKey, c.hpVal = tok.Token, tok.Key, tok.Val
	return nil
}
