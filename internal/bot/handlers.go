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

// handleUpdate despacha un update. El estado se actualiza acá, de forma
// secuencial, y solo la descarga (lo lento) se va a otra goroutine.
func (b *Bot) handleUpdate(ctx context.Context, update tgbotapi.Update) {
	sender := update.SentFrom()
	if sender == nil {
		return // canal, edición de post u otro update sin autor
	}

	chat := update.FromChat()
	if chat == nil {
		return
	}

	if !b.cfg.IsAllowed(sender.ID) {
		log.Printf("acceso denegado al usuario %d (@%s)", sender.ID, sender.UserName)
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
	// Siempre hay que responder el callback, aunque sea vacío, o al usuario le
	// queda el botón girando.
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

	// La lista de géneros se trae una sola vez, acá, y no en el paso del género:
	// así el usuario ve siempre la misma numeración y un API caído no corta la
	// carga a mitad de camino.
	sess := session{Stage: stageAwaitingTitle, Category: cat, Genres: b.genreNames(ctx)}
	b.sessions.set(chatID, sess)

	first, ok := stepFor(sess)
	if !ok { // imposible: todo flujo arranca por el título
		b.send(chatID, "Something went wrong. Use /start to begin again.")
		return
	}
	question := prompt(first, sess)

	// Se edita el menú en lugar de mandar otro mensaje, así no se acumulan
	// teclados viejos que el usuario puede volver a tocar.
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
		// Un sticker, una ubicación, o un paso que espera un archivo y no texto.
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

// handleSkip deja el campo vacío y sigue. Antes /skip se reconocía pero caía
// igual en la máquina de estados, así que el texto "/skip" terminaba guardado
// como si fuera la respuesta.
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

// handleMedia atiende los pasos que esperan un adjunto: la imagen de fondo y
// el archivo final. Antes cualquier adjunto que no llegara en el paso del
// archivo se rechazaba con "Pick a category first", incluso el fondo que el
// bot acababa de pedir.
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

// saveBackground guarda la imagen como background.<ext> dentro de la carpeta
// del título, y deja seguir la conversación sin esperar la descarga.
func (b *Bot) saveBackground(ctx context.Context, msg *tgbotapi.Message, sess session, st step) {
	chatID := msg.Chat.ID

	// Un video o un audio acá es casi siempre el archivo mandado de más temprano.
	if len(msg.Photo) == 0 && msg.Document == nil {
		b.send(chatID, "That doesn't look like an image.\n\n"+prompt(st, sess))
		return
	}

	// La conversación avanza primero y la imagen se baja atrás. Si el usuario
	// contesta el paso siguiente mientras todavía baja, la ruta puede perderse:
	// es una ventana corta y no afecta al archivo, que ya quedó en disco.
	b.advance(chatID, sess)
	b.startDownload(ctx, msg, sess, downloadOpts{
		baseName: "background",
		onSaved: func(path string) {
			b.sessions.update(chatID, func(cur *session) bool {
				if cur.Title != sess.Title { // ya es otra subida
					return false
				}
				cur.Background = path
				return true
			})
		},
	})
}

// saveVideo guarda el archivo final. La sesión queda en este paso a propósito:
// se pueden mandar varios archivos seguidos para el mismo título (el video y
// sus subtítulos, o los episodios de una temporada).
func (b *Bot) saveVideo(ctx context.Context, msg *tgbotapi.Message, sess session, st step) {
	chatID := msg.Chat.ID

	if msg.Video == nil && msg.Document == nil {
		b.send(chatID, "That doesn't look like a video file.\n\n"+prompt(st, sess))
		return
	}

	b.startDownload(ctx, msg, sess, downloadOpts{baseName: videoBaseName(sess)})
}

// videoBaseName arma el nombre del archivo con lo que se cargó en la sesión,
// para que la carpeta quede legible y ordenada: "Title (1999)" para una
// película y "S01E02 - Episode title" para un episodio. Vacío significa que se
// conserva el nombre que trae el archivo.
func videoBaseName(sess session) string {
	if sess.Category != categoryTVShow {
		if sess.ReleaseYear == "" {
			return sess.Title
		}
		return fmt.Sprintf("%s (%s)", sess.Title, sess.ReleaseYear)
	}

	season, errSeason := strconv.Atoi(sess.SeasonNumber)
	episode, errEpisode := strconv.Atoi(sess.EpisodeNumber)
	if errSeason != nil || errEpisode != nil {
		return sess.EpisodeTitle // puede ser "", y entonces manda el nombre original
	}

	name := fmt.Sprintf("S%02dE%02d", season, episode)
	if sess.EpisodeTitle != "" {
		name += " - " + sess.EpisodeTitle
	}
	return name
}

// advance mueve la sesión al paso siguiente y lo pregunta.
func (b *Bot) advance(chatID int64, sess session) {
	next, ok := nextStep(sess)
	if !ok {
		// El último paso espera el archivo y no avanza solo.
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

// --- Flujo de la conversación -------------------------------------------

// step es un paso de la conversación: qué se pregunta, dónde va la respuesta y
// si se puede saltear. El flujo vive en una tabla en vez de en un switch con
// un case por etapa, donde cada bloque repetía la misma asignación y era fácil
// (y así pasó) copiar la del paso anterior.
type step struct {
	stage stage

	// ask arma la pregunta; recibe la sesión para poder nombrar la categoría o
	// el título ya cargado.
	ask func(sess session) string

	// assign valida y guarda la respuesta de texto. El error se le muestra al
	// usuario tal cual y el paso se vuelve a preguntar. nil significa que el
	// paso no acepta texto.
	assign func(sess *session, text string) error

	// media indica que el paso acepta un adjunto.
	media bool

	// skippable habilita /skip.
	skippable bool
}

// metadataSteps es lo que se le pide a cualquier categoría.
var metadataSteps = []step{
	{
		stage: stageAwaitingTitle,
		ask: func(sess session) string {
			return fmt.Sprintf("Send me the title of the %s.", sess.Category.label())
		},
		// El título es el nombre de la carpeta destino: no se puede saltear ni
		// aceptar sin sanear.
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
		skippable: true,
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
				// Sin lista del API se acepta lo que el usuario escriba.
				return "Send me the genres, separated by comma."
			}

			var out strings.Builder
			out.WriteString("Select the genre numbers and send them separated by space:\n")
			for i, name := range sess.Genres {
				fmt.Fprintf(&out, "%d. %s\n", i+1, name)
			}
			return out.String()
		},
		skippable: true,
		assign:    assignGenres,
	},
	{
		stage:     stageAwaitingRating,
		ask:       ask("Send me the rating."),
		skippable: true,
		assign:    func(sess *session, text string) error { sess.Rating = text; return nil },
	},
	{
		stage:     stageAwaitingBackground,
		ask:       ask("Send me the background image, or a link to it."),
		skippable: true,
		media:     true,
		assign:    func(sess *session, text string) error { sess.Background = text; return nil },
	},
}

// episodeSteps solo aplica a series: una película no tiene temporada ni
// episodio, y antes las preguntaba igual.
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

// fileSteps cierra cualquier flujo: lo último que se pide es el archivo.
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

// stepFor ubica el paso en el que está la sesión. Devuelve false si la sesión
// está vacía o vencida.
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

// prompt arma el texto del paso y recuerda /skip donde corresponde, así el
// recordatorio no se puede olvidar en una pregunta suelta.
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

// wholeNumber guarda un entero no negativo normalizado ("07" queda "7").
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

// assignGenres traduce los números que eligió el usuario a los nombres que le
// mostró el paso. Se guardan los nombres y no los números porque la numeración
// es solo de la conversación: el API no la conoce.
func assignGenres(sess *session, text string) error {
	if len(sess.Genres) == 0 {
		sess.Genre = text
		return nil
	}

	// Se acepta "1 3" y también "1, 3": pedir una separación exacta es hacer
	// fallar la carga por una coma.
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
		if seen[n] { // repetir un número no duplica el género
			continue
		}
		seen[n] = true
		chosen = append(chosen, sess.Genres[n-1])
	}

	sess.Genre = strings.Join(chosen, ", ")
	return nil
}

// timecode valida la forma del tiempo y lo guarda tal cual: alcanza con
// rechazar cualquier cosa, sin adivinar en qué unidad lo espera el reproductor.
func timecode(field func(*session) *string) func(*session, string) error {
	return func(sess *session, text string) error {
		if !validTimecode(text) {
			return errors.New("That is not a valid time. Send seconds (90), mm:ss (1:30) or hh:mm:ss.")
		}
		*field(sess) = text
		return nil
	}
}

// validTimecode acepta "90", "1:30" y "1:02:03".
func validTimecode(text string) bool {
	parts := strings.Split(text, ":")
	if len(parts) > 3 {
		return false
	}
	for i, part := range parts {
		// Los segundos sueltos pueden ser cualquier cantidad; con ":" cada
		// grupo es de una o dos cifras.
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

// summary lista lo cargado hasta ahora, para que el usuario lo pueda revisar
// antes de mandar un archivo que puede pesar varios GB.
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
	// genreTTL evita pedirle la lista al API en cada carga.
	genreTTL = 10 * time.Minute
	// genreTimeout acota la espera: el usuario está mirando el menú.
	genreTimeout = 10 * time.Second
)

// genreNames devuelve los nombres de género para numerar en el paso del género.
// Si el API no contesta se devuelve lo último que se pudo traer (o nil, y
// entonces el paso pide el texto a mano): quedarse sin géneros no justifica
// cortar una carga.
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
		log.Printf("no se pudo traer la lista de géneros: %v", err)
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
