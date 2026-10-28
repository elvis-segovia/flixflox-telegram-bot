package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Episode, Season and CatalogItem mirror the flixflox catalog model. _id, uuid,
// created_at and updated_at are not here: the API fills them in when saving,
// and the bot has nothing to fill them with.
type Episode struct {
	EpisodeNumber           int    `json:"episode_number"`
	Title                   string `json:"title,omitempty"`
	FilePath                string `json:"file_path,omitempty"`
	Status                  string `json:"status,omitempty"`
	SkipIntroDisplayMessage string `json:"skip_intro_display_message,omitempty"`
	IntroStartTime          string `json:"intro_start_time,omitempty"`
	IntroEndTime            string `json:"intro_end_time,omitempty"`
	NextEpisodeTime         string `json:"next_episode_time,omitempty"`
}

type Season struct {
	SeasonNumber int       `json:"season_number"`
	Episodes     []Episode `json:"episodes"`
}

type CatalogItem struct {
	Title                   string   `json:"title"`
	Type                    string   `json:"type"`
	ReleaseYear             int      `json:"release_year"`
	Genre                   []string `json:"genre"`
	Rating                  float64  `json:"rating"`
	Description             string   `json:"description,omitempty"`
	Cast                    []string `json:"cast,omitempty"`
	FilePath                string   `json:"file_path,omitempty"`
	BGImage                 string   `json:"bg_image,omitempty"`
	SkipIntroDisplayMessage string   `json:"skip_intro_display_message,omitempty"`
	IntroStartTime          string   `json:"intro_start_time,omitempty"`
	IntroEndTime            string   `json:"intro_end_time,omitempty"`
	Status                  string   `json:"status"`
	Seasons                 []Season `json:"seasons,omitempty"`
}

const (
	// catalogStatus is the status everything the bot uploads comes in with: the
	// file is the original one, not processed yet. The API moves it to "Ready"
	// once it finishes transcoding it.
	catalogStatus = "In-Progress"

	// catalogTimeout caps the wait on the POST: the file is already on disk and
	// nobody is watching this step.
	catalogTimeout = 20 * time.Second
)

// catalogType translates the conversation's category into the value the API
// stores ("tv-shows" is the folder name, not the type).
func catalogType(cat category) string {
	if cat == categoryTVShow {
		return "tvshow"
	}
	return "movie"
}

func setDefault(value string, defaultValue string) string {
	if value == "" {
		return defaultValue
	}
	return value
}

// buildCatalogItem assembles the item for what was just uploaded. A movie
// carries the file at the root; a TV show, inside the single season and the
// single episode this upload brings: the API is the one that merges the episode
// with the ones the show already had.
func (b *Bot) buildCatalogItem(sess session, videoPath string) CatalogItem {
	item := CatalogItem{
		Title:       sess.Title,
		Type:        catalogType(sess.Category),
		ReleaseYear: wholeNumberOf(sess.ReleaseYear),
		Genre:       genreList(sess.Genre),
		Rating:      ratingOf(sess.Rating),
		Description: sess.Description,
		BGImage:     b.catalogPath(sess.Background),
		Status:      catalogStatus,
	}

	if sess.Category != categoryTVShow {
		item.FilePath = b.catalogPath(videoPath)
		return item
	}

	item.Seasons = []Season{{
		SeasonNumber: wholeNumberOf(sess.SeasonNumber),
		Episodes: []Episode{{
			EpisodeNumber:           wholeNumberOf(sess.EpisodeNumber),
			Title:                   sess.EpisodeTitle,
			FilePath:                b.catalogPath(videoPath),
			Status:                  catalogStatus,
			SkipIntroDisplayMessage: setDefault(sess.SkipIntroDisplayMessage, "Skip Intro"),
			IntroStartTime:          setDefault(normalizeTimecode(sess.SkipIntroStartTime), "00:00:00"),
			IntroEndTime:            setDefault(normalizeTimecode(sess.SkipIntroStartTime), "00:00:00"),
			NextEpisodeTime:         setDefault(normalizeTimecode(sess.SkipIntroStartTime), "00:00:00"),
		}},
	}}
	return item
}

// publishCatalogItem sends the item to the API once the file is on disk. The
// file is already safe: if this fails it is reported and the bot carries on,
// because the item can be redone without sending several GB again.
func (b *Bot) publishCatalogItem(ctx context.Context, chatID int64, item CatalogItem) {
	if b.cfg.APIUrl == "" {
		log.Print("FLIXFLOX_API_URL is empty: the catalog item is not published")
		return
	}

	if err := b.postCatalogItem(ctx, item); err != nil {
		log.Printf("error publishing the catalog item for %q: %v", item.Title, err)
		b.send(chatID, "⚠️ The file was saved, but the catalog was not updated:\n"+err.Error())
		return
	}
	b.send(chatID, "📡 Catalog updated for "+item.Title+".")
}

func (b *Bot) postCatalogItem(ctx context.Context, item CatalogItem) error {
	body, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("failed to encode catalog item: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, catalogTimeout)
	defer cancel()

	url := fmt.Sprintf("%s/v1/api/bot/videos", strings.TrimSuffix(b.cfg.APIUrl, "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create catalog request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to post the catalog item: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		answer, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("catalog API answered %d: %s", resp.StatusCode, strings.TrimSpace(string(answer)))
	}
	return nil
}

// catalogPath returns the path the way the API expects it: relative to
// DownloadDir and with "/" ("tv-shows/big_bunny/S01/E01/big_bunny.mkv"). The
// absolute host path is of no use to the API, which mounts the same volume
// somewhere else. A link the user sent instead of an image is left as is.
func (b *Bot) catalogPath(path string) string {
	if path == "" || strings.Contains(path, "://") {
		return path
	}

	rel, err := filepath.Rel(b.cfg.DownloadDir, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

// genreList splits what was stored in the session ("Action, Drama") into the
// array the API stores. It never returns nil: the field is not optional.
func genreList(genre string) []string {
	out := []string{}
	for name := range strings.SplitSeq(genre, ",") {
		if name = strings.TrimSpace(name); name != "" {
			out = append(out, name)
		}
	}
	return out
}

// wholeNumberOf and ratingOf read fields the steps already validated; the zero
// is for a session missing the value, not a way to reject anything.
func wholeNumberOf(text string) int {
	n, err := strconv.Atoi(text)
	if err != nil {
		return 0
	}
	return n
}

func ratingOf(text string) float64 {
	rating, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0
	}
	return rating
}

// normalizeTimecode takes "90", "1:30" and "1:02:03" to the hh:mm:ss form the
// API stores. Text that cannot be read is sent as is: the step already
// validated it, and there is nobody to report it to here.
func normalizeTimecode(text string) string {
	if text == "" {
		return ""
	}

	var total int
	for part := range strings.SplitSeq(text, ":") {
		n, err := strconv.Atoi(part)
		if err != nil {
			return text
		}
		total = total*60 + n
	}
	return fmt.Sprintf("%02d:%02d:%02d", total/3600, total/60%60, total%60)
}
