package bot

import (
	"encoding/json"
	"reflect"
	"testing"

	"flixflox-telegram-bot/internal/config"
)

func testBot() *Bot {
	return &Bot{cfg: &config.Config{DownloadDir: "./uploads"}}
}

// A TV show sends the file inside the episode and a movie at the root: the API
// reads them from different places.
func TestBuildCatalogItemPutsTheEpisodeInsideTheSeason(t *testing.T) {
	sess := session{
		Category:                categoryTVShow,
		Title:                   "Big Bunny",
		ReleaseYear:             "2026",
		Genre:                   "Action, Drama, Romance",
		Rating:                  "5",
		Background:              "uploads/tv-shows/big_bunny/background.jpeg",
		SeasonNumber:            "1",
		EpisodeNumber:           "1",
		EpisodeTitle:            "Pilot",
		SkipIntroDisplayMessage: "Skip Intro",
		SkipIntroStartTime:      "0:30",
		SkipIntroEndTime:        "1:30",
		NexEpisode:              "42:10",
	}

	item := testBot().buildCatalogItem(sess, "uploads/tv-shows/big_bunny/S01/E01/big_bunny.mkv")

	if item.Type != "tvshow" {
		t.Errorf("Type = %q, want tvshow", item.Type)
	}
	if item.ReleaseYear != 2026 || item.Rating != 5 {
		t.Errorf("year/rating = %d/%v, want 2026/5", item.ReleaseYear, item.Rating)
	}
	if want := []string{"Action", "Drama", "Romance"}; !reflect.DeepEqual(item.Genre, want) {
		t.Errorf("Genre = %v, want %v", item.Genre, want)
	}
	if item.Status != "In-Progress" {
		t.Errorf("Status = %q, want In-Progress", item.Status)
	}
	if item.FilePath != "" {
		t.Errorf("a TV show carries no file_path at the root, got %q", item.FilePath)
	}
	if item.BGImage != "tv-shows/big_bunny/background.jpeg" {
		t.Errorf("BGImage = %q, want the relative path", item.BGImage)
	}

	if len(item.Seasons) != 1 || len(item.Seasons[0].Episodes) != 1 {
		t.Fatalf("want one season with one episode, got %+v", item.Seasons)
	}
	episode := item.Seasons[0].Episodes[0]
	if item.Seasons[0].SeasonNumber != 1 || episode.EpisodeNumber != 1 {
		t.Errorf("season/episode = %d/%d, want 1/1", item.Seasons[0].SeasonNumber, episode.EpisodeNumber)
	}
	if episode.FilePath != "tv-shows/big_bunny/S01/E01/big_bunny.mkv" {
		t.Errorf("FilePath = %q, want the path relative to DownloadDir", episode.FilePath)
	}
	if episode.Status != "In-Progress" {
		t.Errorf("episode Status = %q, want In-Progress", episode.Status)
	}
	if episode.IntroStartTime != "00:00:30" || episode.IntroEndTime != "00:01:30" || episode.NextEpisodeTime != "00:42:10" {
		t.Errorf("times = %q/%q/%q, want hh:mm:ss", episode.IntroStartTime, episode.IntroEndTime, episode.NextEpisodeTime)
	}
}

func TestBuildCatalogItemForAMovie(t *testing.T) {
	sess := session{
		Category:    categoryMovie,
		Title:       "Blade Runner",
		ReleaseYear: "1982",
		Genre:       "Sci-Fi",
		Rating:      "8.1",
	}

	item := testBot().buildCatalogItem(sess, "uploads/movies/blade_runner/blade_runner.mkv")

	if item.Type != "movie" {
		t.Errorf("Type = %q, want movie", item.Type)
	}
	if item.FilePath != "movies/blade_runner/blade_runner.mkv" {
		t.Errorf("FilePath = %q, want the path relative to DownloadDir", item.FilePath)
	}
	if item.Seasons != nil {
		t.Errorf("a movie carries no seasons, got %+v", item.Seasons)
	}
}

// The JSON is the contract with the API: the field names matter, and the ones
// the bot does not fill in are not sent empty.
func TestCatalogItemJSON(t *testing.T) {
	sess := session{
		Category:      categoryTVShow,
		Title:         "Big Bunny",
		ReleaseYear:   "2026",
		Genre:         "Action",
		Rating:        "5",
		SeasonNumber:  "1",
		EpisodeNumber: "1",
	}

	body, err := json.Marshal(testBot().buildCatalogItem(sess, "uploads/tv-shows/big_bunny/S01/E01/big_bunny.mkv"))
	if err != nil {
		t.Fatalf("could not serialize: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("could not read the JSON: %v", err)
	}

	for _, field := range []string{"title", "type", "release_year", "genre", "rating", "status", "seasons"} {
		if _, ok := payload[field]; !ok {
			t.Errorf("field %q missing from %s", field, body)
		}
	}
	for _, field := range []string{"_id", "uuid", "description", "cast", "file_path", "bg_image", "created_at", "updated_at"} {
		if _, ok := payload[field]; ok {
			t.Errorf("field %q is not filled in by the bot and was sent anyway: %s", field, body)
		}
	}
}

func TestCatalogPathLeavesLinksAlone(t *testing.T) {
	b := testBot()

	if got := b.catalogPath("https://example.test/bg.jpg"); got != "https://example.test/bg.jpg" {
		t.Errorf("catalogPath = %q, a link is sent as is", got)
	}
	// A file outside DownloadDir cannot be expressed relative to it: it goes whole.
	if got := b.catalogPath("/var/tmp/bg.jpg"); got != "/var/tmp/bg.jpg" {
		t.Errorf("catalogPath = %q, want the path untouched", got)
	}
	if got := b.catalogPath(""); got != "" {
		t.Errorf("catalogPath = %q, want empty", got)
	}
}

func TestNormalizeTimecode(t *testing.T) {
	tests := map[string]string{
		"":        "",
		"90":      "00:01:30",
		"0:30":    "00:00:30",
		"1:30":    "00:01:30",
		"1:02:03": "01:02:03",
		"42:10":   "00:42:10",
	}

	for in, want := range tests {
		if got := normalizeTimecode(in); got != want {
			t.Errorf("normalizeTimecode(%q) = %q, want %q", in, got, want)
		}
	}
}
