package catalog

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"questlog/internal/hltb"
	"questlog/internal/igdb"
	"questlog/internal/steam"
)

// Service combines Steam (primary) and IGDB (fallback for non-Steam
// games) behind one search/details interface, and enriches any match
// with HowLongToBeat "time to beat" data. IGDB participates only when
// Twitch app credentials are configured.
type Service struct {
	steam *steam.Client
	igdb  *igdb.Client
	hltb  *hltb.Client
}

func New(steamClient *steam.Client, igdbClient *igdb.Client, hltbClient *hltb.Client) *Service {
	return &Service{steam: steamClient, igdb: igdbClient, hltb: hltbClient}
}

// Result is one suggestion row in the merged search list.
type Result struct {
	Source   string `json:"source"` // "steam" | "igdb"
	AppID    int64  `json:"appid"`
	Name     string `json:"name"`
	Platform string `json:"platform,omitempty"` // IGDB platform hint
}

// Search queries Steam and IGDB (when configured) in parallel and
// merges the results, exact name matches first, then shorter names.
func (s *Service) Search(ctx context.Context, q string) []Result {
	type outcome struct {
		items []Result
	}

	ch := make(chan outcome, 2)
	go func() {
		items := []Result{}
		if res, err := s.steam.Search(ctx, q); err == nil {
			for _, r := range res {
				items = append(items, Result{Source: "steam", AppID: r.AppID, Name: r.Name})
			}
		}
		ch <- outcome{items: items}
	}()
	go func() {
		items := []Result{}
		if s.igdb != nil && s.igdb.Enabled() {
			if res, err := s.igdb.Search(ctx, q, 6); err == nil {
				for _, r := range res {
					items = append(items, Result{
						Source: "igdb", AppID: r.AppID, Name: r.Name, Platform: r.Platform,
					})
				}
			}
		}
		ch <- outcome{items: items}
	}()

	results := []Result{}
	for i := 0; i < 2; i++ {
		o := <-ch
		results = append(results, o.items...)
	}
	return mergeResults(results, nil, q)
}

// mergeResults combines Steam + IGDB matches: exact name match first,
// then shorter names, capped at 12 rows.
func mergeResults(steamResults, igdbResults []Result, q string) []Result {
	results := append([]Result{}, steamResults...)
	results = append(results, igdbResults...)

	lower := strings.ToLower(strings.TrimSpace(q))
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
	if len(results) > 12 {
		results = results[:12]
	}
	return results
}

// Details is the enriched data for one catalog entry, whichever source
// it came from.
type Details struct {
	Source            string   `json:"source"`
	AppID             int64    `json:"appid"`
	Name              string   `json:"name"`
	CoverURL          string   `json:"coverUrl"`
	Year              *int     `json:"year"`
	Genre             string   `json:"genre"`
	Platform          string   `json:"platform"`
	Description       string   `json:"description"`
	Developers        []string `json:"developers,omitempty"`
	Metacritic        *int     `json:"metacritic,omitempty"`
	TimeToBeatMinutes *int     `json:"timeToBeatMinutes,omitempty"` // HowLongToBeat main story
}

// Details resolves a single catalog entry by source + id.
func (s *Service) Details(ctx context.Context, source string, appID int64) (*Details, error) {
	var details *Details
	switch source {
	case "steam":
		d, err := s.steam.AppDetails(ctx, appID)
		if err != nil {
			return nil, err
		}
		details = &Details{
			Source: "steam", AppID: d.AppID, Name: d.Name, CoverURL: d.CoverURL,
			Year: d.Year, Genre: d.Genre, Platform: d.Platform,
			Description: d.Description, Developers: d.Developers, Metacritic: d.Metacritic,
		}

	case "igdb":
		if s.igdb == nil || !s.igdb.Enabled() {
			return nil, errors.New("igdb is not configured (set IGDB_CLIENT_ID and IGDB_CLIENT_SECRET)")
		}
		d, err := s.igdb.AppDetails(ctx, appID)
		if err != nil {
			return nil, err
		}
		details = &Details{
			Source: "igdb", AppID: d.AppID, Name: d.Name, CoverURL: d.CoverURL,
			Year: d.Year, Genre: d.Genre, Platform: d.Platform,
			Description: d.Description,
		}

	default:
		return nil, fmt.Errorf("unknown source %q (use steam or igdb)", source)
	}

	s.enrichTimeToBeat(ctx, details)
	return details, nil
}

// enrichTimeToBeat looks up the game's main-story completion time on
// HowLongToBeat and attaches it to the details. Best-effort: a lookup
// failure or missing data leaves the field nil rather than failing the
// whole catalog call. The lookup runs under its own tight timeout so
// a stalled HLTB (cold session = 3 round trips, plus a possible 403
// retry) never holds the user-facing details response hostage.
func (s *Service) enrichTimeToBeat(ctx context.Context, d *Details) {
	if s.hltb == nil {
		return
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	res, err := s.hltb.Search(lookupCtx, d.Name)
	if err != nil {
		log.Printf("hltb lookup for %q: %v", d.Name, err)
		return
	}
	d.TimeToBeatMinutes = res.TimeToBeatMinutes
}
