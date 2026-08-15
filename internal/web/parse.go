package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/leoarkiteto/questlog-api/internal/model"
)

// urlQuery escapes a value for use in a query string.
func urlQuery(s string) string {
	return url.QueryEscape(s)
}

// parseGameForm reads the add/edit form (multipart or urlencoded) into
// a model.Game. Missing values become zero values; the caller validates.
func parseGameForm(r *http.Request) *model.Game {
	if err := r.ParseForm(); err != nil {
		return &model.Game{}
	}
	return &model.Game{
		Title:             strings.TrimSpace(r.FormValue("title")),
		Status:            model.Status(r.FormValue("status")),
		Rating:            formInt(r.FormValue("rating")),
		Platform:          strings.TrimSpace(r.FormValue("platform")),
		Year:              formOptionalInt(r.FormValue("year")),
		Genre:             strings.TrimSpace(r.FormValue("genre")),
		CoverURL:          strings.TrimSpace(r.FormValue("coverUrl")),
		Description:       strings.TrimSpace(r.FormValue("description")),
		Notes:             strings.TrimSpace(r.FormValue("notes")),
		SteamAppID:        formOptionalInt64(r.FormValue("steamAppId")),
		TimeToBeatMinutes: formOptionalInt(r.FormValue("timeToBeatMinutes")),
	}
}

func formInt(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func formOptionalInt(s string) *int {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return nil
	}
	return &n
}

func formOptionalInt64(s string) *int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil
	}
	return &n
}
