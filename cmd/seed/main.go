// Command seed inserts a few sample games so the dashboard isn't empty.
// Steam titles get real Steam cover art + steam_appid; the rest get a
// text placeholder. Duplicates (same title/appid) are skipped.
//
//	cd questlog-api && go run ./cmd/seed
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/leoarkiteto/questlog-api/internal/config"
	"github.com/leoarkiteto/questlog-api/internal/model"
	"github.com/leoarkiteto/questlog-api/internal/repo"
)

type seedGame struct {
	title    string
	status   model.Status
	rating   int
	platform string
	year     *int
	genre    string
	appID    *int64
}

func y(n int) *int     { return &n }
func a(n int64) *int64 { return &n }

var seedGames = []seedGame{
	{"Elden Ring", model.StatusPlayed, 5, "PC", y(2022), "Action RPG", a(1245620)},
	{"The Witcher 3", model.StatusPlayed, 5, "PlayStation 5", y(2015), "Action RPG", a(292030)},
	{"Stardew Valley", model.StatusPlayed, 4, "PC", y(2016), "Simulation", a(413150)},
	{"Hollow Knight", model.StatusPlayed, 5, "Nintendo Switch", y(2017), "Metroidvania", a(367520)},
	{"Cyberpunk 2077", model.StatusPlayed, 3, "PC", y(2020), "Action RPG", a(1091500)},
	{"Zelda: Tears of the Kingdom", model.StatusPlaying, 0, "Nintendo Switch", y(2023), "Adventure", nil},
	{"Balatro", model.StatusPlaying, 0, "Mobile (iOS)", y(2024), "Roguelike", a(2379780)},
	{"Silksong", model.StatusWishlist, 0, "Nintendo Switch", nil, "Metroidvania", a(1030300)},
	{"GTA VI", model.StatusWishlist, 0, "PlayStation 5", nil, "Open World", nil},
	{"Half-Life 3", model.StatusWishlist, 0, "PC", nil, "FPS", nil},
	{"Baldur's Gate 3", model.StatusPurchased, 0, "PC", y(2023), "CRPG", a(1086940)},
	{"Starfield", model.StatusDropped, 2, "PC", y(2023), "Space RPG", nil},
}

func main() {
	_ = config.LoadDotEnv(".env")

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://gamelog:gamelog@localhost:5432/gamelog"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store, err := repo.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		log.Fatalf("migrations: %v", err)
	}

	for _, s := range seedGames {
		g := &model.Game{
			Title:    s.title,
			Status:   s.status,
			Rating:   s.rating,
			Platform: s.platform,
			Year:     s.year,
			Genre:    s.genre,
			CoverURL: coverFor(s),
			Notes:    "Sample entry — edit or delete me anytime.",
		}
		if s.appID != nil {
			g.SteamAppID = s.appID
		}
		if _, err := store.Create(ctx, g); err != nil {
			var dup *repo.ErrDuplicate
			if errors.As(err, &dup) {
				fmt.Printf("  = %s (already present, skipped)\n", s.title)
				continue
			}
			log.Printf("  ! %s: %v", s.title, err)
			continue
		}
		fmt.Printf("  + %s\n", s.title)
	}
	fmt.Println("Done.")
}

func coverFor(s seedGame) string {
	if s.appID != nil {
		return fmt.Sprintf(
			"https://shared.akamai.steamstatic.com/store_item_assets/steam/apps/%d/library_600x900.jpg",
			*s.appID,
		)
	}
	return ""
}
