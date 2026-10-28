package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"flixflox-telegram-bot/internal/storage"
)

type Genre struct {
	Genre string `json:"genre"`
}

const helpText = "Commands:\n" +
	"/start - choose what to upload\n" +
	"/cancel - forget the current upload\n" +
	"/skip - skip the field\n" +
	"/help - show this message"

// handleUpdate dispatches an update. The state is updated here, sequentially,
// and only the download (the slow part) goes to another goroutine.
func (b *Bot) handleUpdate(ctx context.Context, update tgbotapi.Update) {
	sender := update.SentFrom()
	if sender == nil {
		return // channel, edited post or another update with no author
	}

	chat := update.FromChat()
	if chat == nil {
		return
	}

	if !b.cfg.IsAllowed(sender.ID) {
		log.Printf("access denied to user %d (@%s)", sender.ID, sender.UserName)
		if update.CallbackQuery != nil {
			b.answerCallback(update.CallbackQuery.ID, "Not allowed")
			return
		}
		b.send(chat.ID, "You are not allowed to use this bot.")
		return
	}

	switch {
	case update.CallbackQuery != nil:
		b.handleCallback(ctx, update.CallbackQuery)
	case update.Message != nil:
		b.handleMessage(ctx, update.Message)
	}
}

func (b *Bot) handleCallback(ctx context.Context, callback *tgbotapi.CallbackQuery) {
	// The callback always has to be answered, even with an empty text, or the
	// user is left with the button spinning.
	defer b.answerCallback(callback.ID, "")

	if callback.Message == nil {
		return
	}
	chatID := callback.Message.Chat.ID

	var cat category
	switch category(callback.Data) {
	case categoryMovie:
		cat = categoryMovie
	case categoryTVShow:
		cat = categoryTVShow
	default:
		b.send(chatID, "Unknown option. Use /start to begin again.")
		return
	}

	// The genre list is fetched once, here, and not at the genre step: that way
	// the user always sees the same numbering, and an API that is down does not
	// cut the upload off halfway through.
	sess := session{Stage: stageAwaitingTitle, Category: cat, Genres: b.genreNames(ctx)}
	b.sessions.set(chatID, sess)

	first, ok := stepFor(sess)
	if !ok { // impossible: every flow starts with the title
		b.send(chatID, "Something went wrong. Use /start to begin again.")
		return
	}
	question := prompt(first, sess)

	// The menu is edited instead of sending another message, so old keyboards
	// the user could tap again do not pile up.
	edit := tgbotapi.NewEditMessageText(chatID, callback.Message.MessageID, question)
	if _, err := b.api.Send(edit); err != nil {
		log.Printf("error editing the menu: %v", err)
		b.send(chatID, question)
	}
}

func (b *Bot) handleMessage(ctx context.Context, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	sess := b.sessions.get(chatID)

	if msg.IsCommand() {
		b.handleCommand(chatID, msg.Command(), sess)
		return
	}

	st, inFlow := stepFor(sess)
	if !inFlow {
		b.send(chatID, "Use /start to upload a movie or TV show.")
		return
	}

	if hasMedia(msg) {
		b.handleMedia(ctx, msg, sess, st)
		return
	}

	text := strings.TrimSpace(msg.Text)
	if text == "" || st.assign == nil {
		// A sticker, a location, or a step that expects a file and not text.
		b.send(chatID, prompt(st, sess))
		return
	}

	if err := st.assign(&sess, text); err != nil {
		b.send(chatID, err.Error()+"\n\n"+prompt(st, sess))
		return
	}
	b.advance(chatID, sess)
}

func (b *Bot) handleCommand(chatID int64, command string, sess session) {
	switch command {
	case "start":
		b.sessions.clear(chatID)
		reply := tgbotapi.NewMessage(chatID, "What do you want to upload?")
		reply.ReplyMarkup = mainMenu()
		b.deliver(reply)
	case "cancel":
		b.sessions.clear(chatID)
		b.send(chatID, "Cancelled. Use /start to upload something else.")
	case "help":
		b.send(chatID, helpText)
	case "skip":
		b.handleSkip(chatID, sess)
	default:
		b.send(chatID, "Unknown command.\n\n"+helpText)
	}
}

// handleSkip leaves the field empty and moves on. /skip used to be recognized
// but fell through to the state machine anyway, so the text "/skip" ended up
// stored as if it were the answer.
func (b *Bot) handleSkip(chatID int64, sess session) {
	st, ok := stepFor(sess)
	if !ok {
		b.send(chatID, "There is nothing to skip. Use /start to upload something.")
		return
	}
	if !st.skippable {
		b.send(chatID, "This one can't be skipped.\n\n"+prompt(st, sess))
		return
	}
	b.advance(chatID, sess)
}

// handleMedia serves the steps that expect an attachment: the background image
// and the final file. Any attachment that did not arrive at the file step used
// to be rejected with "Pick a category first", even the background the bot had
// just asked for.
func (b *Bot) handleMedia(ctx context.Context, msg *tgbotapi.Message, sess session, st step) {
	chatID := msg.Chat.ID

	if !st.media {
		b.send(chatID, "I wasn't expecting a file here.\n\n"+prompt(st, sess))
		return
	}

	if st.stage == stageAwaitingBackground {
		b.saveBackground(ctx, msg, sess, st)
		return
	}
	b.saveVideo(ctx, msg, sess, st)
}

// saveBackground stores the image as background.<ext> inside the title's
// folder, and lets the conversation carry on without waiting for the download.
func (b *Bot) saveBackground(ctx context.Context, msg *tgbotapi.Message, sess session, st step) {
	chatID := msg.Chat.ID

	// A video or an audio here is almost always the file sent too early.
	if len(msg.Photo) == 0 && msg.Document == nil {
		b.send(chatID, "That doesn't look like an image.\n\n"+prompt(st, sess))
		return
	}

	// The conversation moves on first and the image is fetched behind it. If
	// the user answers the next step while it is still downloading, the path
	// can be lost: it is a short window and does not affect the file, which is
	// already on disk.
	b.advance(chatID, sess)
	b.startDownload(ctx, msg, sess, downloadOpts{
		baseName: "background",
		kind:     "background",
		onSaved: func(path string) {
			b.sessions.update(chatID, func(cur *session) bool {
				if cur.Title != sess.Title { // this is another upload already
					return false
				}
				cur.Background = path
				return true
			})
		},
	})
}

// saveVideo stores the final file and publishes the item in the catalog. The
// session stays on this step on purpose: several files can be sent in a row for
// the same title (the video and its subtitles, or the episodes of a season),
// and each one publishes its own item.
func (b *Bot) saveVideo(ctx context.Context, msg *tgbotapi.Message, sess session, st step) {
	chatID := msg.Chat.ID

	if msg.Video == nil && msg.Document == nil {
		b.send(chatID, "That doesn't look like a video file.\n\n"+prompt(st, sess))
		return
	}

	b.startDownload(ctx, msg, sess, downloadOpts{
		baseName: sess.Title,
		kind:     video,
		onSaved: func(path string) {
			// The background is fetched in parallel and may have finished after
			// the conversation moved on: the session is re-read so the item is
			// not published without its bg_image.
			if cur := b.sessions.get(chatID); cur.Title == sess.Title && cur.Background != "" {
				sess.Background = cur.Background
			}
			b.publishCatalogItem(ctx, chatID, b.buildCatalogItem(sess, path))
		},
	})
}

// advance moves the session to the next step and asks it.
func (b *Bot) advance(chatID int64, sess session) {
	next, ok := nextStep(sess)
	if !ok {
		// The last step waits for the file and does not advance on its own.
		b.sessions.set(chatID, sess)
		return
	}

	sess.Stage = next.stage
	b.sessions.set(chatID, sess)
	b.send(chatID, prompt(next, sess))
}

func mainMenu() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Movie", string(categoryMovie)),
			tgbotapi.NewInlineKeyboardButtonData("Tv Show", string(categoryTVShow)),
		),
	)
}

// --- Conversation flow ---------------------------------------------------

// step is one step of the conversation: what is asked, where the answer goes
// and whether it can be skipped. The flow lives in a table instead of a switch
// with one case per stage, where every block repeated the same assignment and
// it was easy (and it did happen) to copy the one from the previous step.
type step struct {
	stage stage

	// ask builds the question; it takes the session so it can name the category
	// or the title already entered.
	ask func(sess session) string

	// assign validates and stores the text answer. The error is shown to the
	// user as is and the step is asked again. nil means the step takes no text.
	assign func(sess *session, text string) error

	// media marks a step that accepts an attachment.
	media bool

	// skippable enables /skip.
	skippable bool
}

// metadataSteps is what every category is asked for.
var metadataSteps = []step{
	{
		stage: stageAwaitingTitle,
		ask: func(sess session) string {
			return fmt.Sprintf("Send me the title of the %s.", sess.Category.label())
		},
		// The title is the name of the destination folder: it can neither be
		// skipped nor accepted without sanitizing.
		assign: func(sess *session, text string) error {
			title := storage.SanitizeName(text)
			if title == "" {
				return errors.New("That title can't be used as a folder name.")
			}
			sess.Title = title
			return nil
		},
	},
	{
		stage:     stageAwaitingDescription,
		ask:       ask("Now send me the description."),
		skippable: true,
		assign:    func(sess *session, text string) error { sess.Description = text; return nil },
	},
	{
		stage:     stageAwaitingYear,
		ask:       ask("Send me the release year."),
		skippable: false,
		assign: func(sess *session, text string) error {
			year, err := strconv.Atoi(text)
			if err != nil || year < 1880 || year > 2100 {
				return errors.New("That is not a valid year. Send four digits, like 1999.")
			}
			sess.ReleaseYear = strconv.Itoa(year)
			return nil
		},
	},
	{
		stage: stageAwaitingGenre,
		ask: func(sess session) string {
			if len(sess.Genres) == 0 {
				// Without the API's list, whatever the user types is accepted.
				return "Send me the genres, separated by comma."
			}

			var out strings.Builder
			out.WriteString("Select the genre numbers and send them separated by space:\n")
			for i, name := range sess.Genres {
				fmt.Fprintf(&out, "%d. %s\n", i+1, name)
			}
			return out.String()
		},
		skippable: false,
		assign:    assignGenres,
	},
	{
		stage:     stageAwaitingRating,
		ask:       ask("Send me the rating."),
		skippable: false,
		// The catalog stores it as a number: loose text here used to end up
		// published as rating 0 without anyone noticing.
		assign: func(sess *session, text string) error {
			rating, err := strconv.ParseFloat(strings.Replace(text, ",", ".", 1), 64)
			if err != nil || rating < 0 || rating > 10 {
				return errors.New("That is not a valid rating. Send a number between 0 and 10, like 8.1.")
			}
			sess.Rating = strconv.FormatFloat(rating, 'f', -1, 64)
			return nil
		},
	},
	{
		stage:     stageAwaitingBackground,
		ask:       ask("Send me the background image, or a link to it."),
		skippable: false,
		media:     true,
		assign:    func(sess *session, text string) error { sess.Background = text; return nil },
	},
}

// episodeSteps only applies to TV shows: a movie has no season or episode, and
// it used to be asked for them anyway.
var episodeSteps = []step{
	{
		stage:  stageAwaitingSeasonNumber,
		ask:    ask("Send me the season number."),
		assign: wholeNumber("season number", func(sess *session) *string { return &sess.SeasonNumber }),
	},
	{
		stage:  stageAwaitingEpisodeNumber,
		ask:    ask("Send me the episode number."),
		assign: wholeNumber("episode number", func(sess *session) *string { return &sess.EpisodeNumber }),
	},
	{
		stage:     stageAwaitingEpisodeTitle,
		ask:       ask("Send me the episode title."),
		skippable: true,
		assign:    func(sess *session, text string) error { sess.EpisodeTitle = text; return nil },
	},
	{
		stage:     stageAwaitingSkipIntroDisplayMessage,
		ask:       ask("Send me the label for the skip-intro button."),
		skippable: true,
		assign: func(sess *session, text string) error {
			sess.SkipIntroDisplayMessage = text
			return nil
		},
	},
	{
		stage:     stageAwaitingSkipIntroStartTime,
		ask:       ask("Send me the skip-intro start time (mm:ss)."),
		skippable: true,
		assign:    timecode(func(sess *session) *string { return &sess.SkipIntroStartTime }),
	},
	{
		stage:     stageAwaitingSkipIntroEndTime,
		ask:       ask("Send me the skip-intro end time (mm:ss)."),
		skippable: true,
		assign:    timecode(func(sess *session) *string { return &sess.SkipIntroEndTime }),
	},
	{
		stage:     stageAwaitingNextEpisode,
		ask:       ask("Send me the time when the next-episode button shows up (mm:ss)."),
		skippable: true,
		assign:    timecode(func(sess *session) *string { return &sess.NexEpisode }),
	},
}

// fileSteps closes every flow: the last thing asked for is the file.
var fileSteps = []step{
	{
		stage: stageAwaitingFile,
		ask: func(sess session) string {
			return sess.summary() + "\nNow send me the video file."
		},
		media: true,
	},
}

var (
	movieFlow  = joinSteps(metadataSteps, fileSteps)
	tvShowFlow = joinSteps(metadataSteps, episodeSteps, fileSteps)
)

func joinSteps(parts ...[]step) []step {
	var out []step
	for _, part := range parts {
		out = append(out, part...)
	}
	return out
}

func flowFor(cat category) []step {
	if cat == categoryTVShow {
		return tvShowFlow
	}
	return movieFlow
}

// stepFor locates the step the session is on. It returns false when the
// session is empty or expired.
func stepFor(sess session) (step, bool) {
	for _, st := range flowFor(sess.Category) {
		if st.stage == sess.Stage {
			return st, true
		}
	}
	return step{}, false
}

func nextStep(sess session) (step, bool) {
	flow := flowFor(sess.Category)
	for i, st := range flow {
		if st.stage == sess.Stage && i+1 < len(flow) {
			return flow[i+1], true
		}
	}
	return step{}, false
}

// prompt builds the step's text and mentions /skip where it applies, so the
// reminder cannot be forgotten on a single question.
func prompt(st step, sess session) string {
	text := st.ask(sess)
	if st.skippable {
		text += "\nSend /skip to leave it empty."
	}
	return text
}

func ask(text string) func(session) string {
	return func(session) string { return text }
}

// wholeNumber stores a normalized non-negative integer ("07" becomes "7").
func wholeNumber(label string, field func(*session) *string) func(*session, string) error {
	return func(sess *session, text string) error {
		n, err := strconv.Atoi(text)
		if err != nil || n < 0 {
			return fmt.Errorf("That is not a valid %s. Send a number, like 1.", label)
		}
		*field(sess) = strconv.Itoa(n)
		return nil
	}
}

// assignGenres translates the numbers the user picked into the names the step
// showed them. The names are stored and not the numbers because the numbering
// belongs to the conversation only: the API does not know it.
func assignGenres(sess *session, text string) error {
	if len(sess.Genres) == 0 {
		sess.Genre = text
		return nil
	}

	// Both "1 3" and "1, 3" are accepted: demanding an exact separator means
	// failing an upload over a comma.
	numbers := strings.FieldsFunc(text, func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	})
	if len(numbers) == 0 {
		return fmt.Errorf("Send at least one number between 1 and %d.", len(sess.Genres))
	}

	var chosen []string
	seen := make(map[int]bool, len(numbers))
	for _, number := range numbers {
		n, err := strconv.Atoi(number)
		if err != nil || n < 1 || n > len(sess.Genres) {
			return fmt.Errorf("%q is not on the list. Send numbers between 1 and %d.", number, len(sess.Genres))
		}
		if seen[n] { // repeating a number does not duplicate the genre
			continue
		}
		seen[n] = true
		chosen = append(chosen, sess.Genres[n-1])
	}

	sess.Genre = strings.Join(chosen, ", ")
	return nil
}

// timecode validates the shape of the time and stores it as is: rejecting
// anything else is enough, without guessing which unit the player expects.
func timecode(field func(*session) *string) func(*session, string) error {
	return func(sess *session, text string) error {
		if !validTimecode(text) {
			return errors.New("That is not a valid time. Send seconds (90), mm:ss (1:30) or hh:mm:ss.")
		}
		*field(sess) = text
		return nil
	}
}

// validTimecode accepts "90", "1:30" and "1:02:03".
func validTimecode(text string) bool {
	parts := strings.Split(text, ":")
	if len(parts) > 3 {
		return false
	}
	for i, part := range parts {
		// Bare seconds can be any amount; with ":" each group is one or two
		// digits.
		maxLen := 2
		if len(parts) == 1 {
			maxLen = 6
		}
		if len(part) == 0 || len(part) > maxLen {
			return false
		}
		if i > 0 && len(part) != 2 {
			return false
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

// summary lists what has been entered so far, so the user can check it before
// sending a file that may weigh several GB.
func (s session) summary() string {
	fields := []struct{ label, value string }{
		{"Title", s.Title},
		{"Description", s.Description},
		{"Year", s.ReleaseYear},
		{"Genre", s.Genre},
		{"Rating", s.Rating},
		{"Background", s.Background},
		{"Season", s.SeasonNumber},
		{"Episode", s.EpisodeNumber},
		{"Episode title", s.EpisodeTitle},
		{"Skip intro label", s.SkipIntroDisplayMessage},
		{"Skip intro from", s.SkipIntroStartTime},
		{"Skip intro to", s.SkipIntroEndTime},
		{"Next episode at", s.NexEpisode},
	}

	var out strings.Builder
	fmt.Fprintf(&out, "This is the %s so far:\n", s.Category.label())
	for _, f := range fields {
		if f.value == "" {
			continue
		}
		fmt.Fprintf(&out, "• %s: %s\n", f.label, f.value)
	}
	return out.String()
}

const (
	// genreTTL avoids asking the API for the list on every upload.
	genreTTL = 10 * time.Minute
	// genreTimeout caps the wait: the user is staring at the menu.
	genreTimeout = 10 * time.Second
)

// genreNames returns the genre names to number at the genre step. If the API
// does not answer, the last ones fetched are returned (or nil, and then the
// step asks for the text by hand): running out of genres is no reason to cut an
// upload short.
func (b *Bot) genreNames(ctx context.Context) []string {
	b.genresMu.Lock()
	defer b.genresMu.Unlock()

	if b.genres != nil && time.Since(b.genresAt) < genreTTL {
		return b.genres
	}

	ctx, cancel := context.WithTimeout(ctx, genreTimeout)
	defer cancel()

	genres, err := b.fetchGenreData(ctx)
	if err != nil {
		log.Printf("could not fetch the genre list: %v", err)
		return b.genres
	}

	names := make([]string, 0, len(genres))
	for _, genre := range genres {
		if name := strings.TrimSpace(genre.Genre); name != "" {
			names = append(names, name)
		}
	}

	b.genres = names
	b.genresAt = time.Now()
	return names
}

func (b *Bot) fetchGenreData(ctx context.Context) ([]Genre, error) {
	url := fmt.Sprintf("%s/v1/api/bot/genres", b.cfg.APIUrl)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create genre request: %w", err)
	}

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch flixflox genre data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("failed to fetch flixflox genre data: status %d: %s", resp.StatusCode, body)
	}

	var genres []Genre
	if err := json.NewDecoder(resp.Body).Decode(&genres); err != nil {
		return nil, fmt.Errorf("failed to decode genre data: %w", err)
	}

	return genres, nil
}
