package model

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Status is the collection a game belongs to.
type Status string

const (
	StatusWishlist  Status = "wishlist"  // I wish to play/buy
	StatusPurchased Status = "purchased" // bought but not played yet
	StatusPlaying   Status = "playing"   // currently playing
	StatusPlayed    Status = "played"    // already played
	StatusDropped   Status = "dropped"   // tried, didn't finish
)

// AllStatuses lists every valid status, in display order.
var AllStatuses = []Status{StatusWishlist, StatusPurchased, StatusPlaying, StatusPlayed, StatusDropped}

func (s Status) Valid() bool {
	switch s {
	case StatusWishlist, StatusPurchased, StatusPlaying, StatusPlayed, StatusDropped:
		return true
	}
	return false
}

// Display returns a human-friendly label for the status.
func (s Status) Display() string {
	switch s {
	case StatusWishlist:
		return "Wish to Play / Buy"
	case StatusPurchased:
		return "Purchased"
	case StatusPlaying:
		return "Currently Playing"
	case StatusPlayed:
		return "Played"
	case StatusDropped:
		return "Dropped"
	}
	return string(s)
}

// Game is a single entry in the collection.
type Game struct {
	ID                int64     `json:"id"`
	Title             string    `json:"title"`
	Status            Status    `json:"status"`
	Rating            int       `json:"rating"` // 0 = unrated, 1..5 stars
	Platform          string    `json:"platform"`
	Year              *int      `json:"year"`
	Genre             string    `json:"genre"`
	CoverURL          string    `json:"coverUrl"`
	Description       string    `json:"description"`
	Notes             string    `json:"notes"`
	SteamAppID        *int64    `json:"steamAppId"`
	TimeToBeatMinutes *int      `json:"timeToBeatMinutes"` // HowLongToBeat main story
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

// Validate checks the fields supplied by the client.
func (g *Game) Validate() error {
	if strings.TrimSpace(g.Title) == "" {
		return errors.New("title is required")
	}
	if !g.Status.Valid() {
		return fmt.Errorf("invalid status %q (must be one of: wishlist, purchased, playing, played, dropped)", g.Status)
	}
	if g.Rating < 0 || g.Rating > 5 {
		return errors.New("rating must be between 0 and 5")
	}
	if g.Year != nil && (*g.Year < 1950 || *g.Year > 2100) {
		return errors.New("year out of range")
	}
	if g.TimeToBeatMinutes != nil && (*g.TimeToBeatMinutes < 1 || *g.TimeToBeatMinutes > 100000) {
		return errors.New("time to beat out of range (1 minute to ~69 days)")
	}
	return nil
}
