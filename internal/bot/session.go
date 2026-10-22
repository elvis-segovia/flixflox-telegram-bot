package bot

import (
	"sync"
	"time"
)

// sessionTTL descarta conversaciones abandonadas para que el estado no quede
// pegado si alguien elige "Movie" y nunca manda el archivo.
const sessionTTL = time.Hour

type stage int

const (
	stageIdle stage = iota
	stageAwaitingTitle
	stageAwaitingDescription
	stageAwaitingYear
	stageAwaitingGenre
	stageAwaitingRating
	stageAwaitingBackground
	stageAwaitingSeasonNumber
	stageAwaitingEpisodeTitle
	stageAwaitingEpisodeNumber
	stageAwaitingSkipIntroDisplayMessage
	stageAwaitingSkipIntroStartTime
	stageAwaitingSkipIntroEndTime
	stageAwaitingNextEpisode
	stageAwaitingFile
)

type category string

const (
	categoryMovie  category = "movie"
	categoryTVShow category = "tv-show"
)

// dir es el subdirectorio de uploads/ donde cae cada categoría.
func (c category) dir() string {
	switch c {
	case categoryMovie:
		return "movies"
	case categoryTVShow:
		return "tv-shows"
	default:
		return "other"
	}
}

func (c category) label() string {
	switch c {
	case categoryMovie:
		return "movie"
	case categoryTVShow:
		return "TV show"
	default:
		return string(c)
	}
}

type session struct {
	Stage       stage
	Category    category
	Title       string
	Description string
	ReleaseYear string
	Genre       string
	// Genres es la lista que le mostró el bot al elegir categoría. Queda fija
	// durante toda la carga para que los números que mande el usuario sigan
	// apuntando a los mismos géneros aunque el API cambie mientras tanto.
	Genres                  []string
	Rating                  string
	Background              string
	SeasonNumber            string
	EpisodeNumber           string
	EpisodeTitle            string
	SkipIntroDisplayMessage string
	SkipIntroStartTime      string
	SkipIntroEndTime        string
	NexEpisode              string
	UpdatedAt               time.Time
}

// sessionStore guarda el estado de cada chat. Es seguro para uso concurrente.
type sessionStore struct {
	mu   sync.Mutex
	data map[int64]*session
}

func newSessionStore() *sessionStore {
	return &sessionStore{data: make(map[int64]*session)}
}

// get devuelve una copia del estado del chat. Una sesión vencida se descarta.
func (s *sessionStore) get(chatID int64) session {
	s.mu.Lock()
	defer s.mu.Unlock()

	cur, ok := s.data[chatID]
	if !ok {
		return session{Stage: stageIdle}
	}
	if time.Since(cur.UpdatedAt) > sessionTTL {
		delete(s.data, chatID)
		return session{Stage: stageIdle}
	}
	return *cur
}

func (s *sessionStore) set(chatID int64, sess session) {
	sess.UpdatedAt = time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	s.data[chatID] = &sess
	s.sweepLocked()
}

// update aplica fn sobre la sesión guardada, si todavía existe y no venció.
// Lo usa la descarga de la imagen de fondo, que termina después de que la
// conversación siguió adelante. fn devuelve false si no tocó nada.
func (s *sessionStore) update(chatID int64, fn func(*session) bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cur, ok := s.data[chatID]
	if !ok || time.Since(cur.UpdatedAt) > sessionTTL {
		return
	}
	if fn(cur) {
		cur.UpdatedAt = time.Now()
	}
}

func (s *sessionStore) clear(chatID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.data, chatID)
}

// sweepLocked limpia sesiones vencidas para que el mapa no crezca sin control.
// El llamador debe tener el mutex tomado.
func (s *sessionStore) sweepLocked() {
	if len(s.data) < 64 {
		return
	}
	for id, sess := range s.data {
		if time.Since(sess.UpdatedAt) > sessionTTL {
			delete(s.data, id)
		}
	}
}
