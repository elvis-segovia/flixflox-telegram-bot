package bot

import (
	"sync"
	"time"
)

// sessionTTL drops abandoned conversations so the state does not stay stuck
// when someone picks "Movie" and never sends the file.
const sessionTTL = time.Hour

type stage int

const (
	stageIdle stage = iota
	stageAwaitingTitle
	stageAwaitingTitleList
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
	categoryTVShow category = "tv-shows"
	newEpisode     category = "new-episode"
)

// dir is the subdirectory of uploads/ where each category lands.
func (c category) dir() string {
	switch c {
	case categoryMovie:
		return "movies"
	// A new episode is a file of a show already uploaded: it belongs next to
	// the other episodes, not in a folder of its own.
	case categoryTVShow, newEpisode:
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
	case newEpisode:
		return "new episode"
	default:
		return string(c)
	}
}

type session struct {
	Stage                   stage
	Category                category
	UUID                    string
	Title                   string
	Titles                  []Title
	Description             string
	ReleaseYear             string
	Genre                   string
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

// sessionStore keeps the state of each chat. It is safe for concurrent use.
type sessionStore struct {
	mu   sync.Mutex
	data map[int64]*session
}

func newSessionStore() *sessionStore {
	return &sessionStore{data: make(map[int64]*session)}
}

// get returns a copy of the chat's state. An expired session is discarded.
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

// update applies fn to the stored session, if it still exists and has not
// expired. It is used by the background image download, which finishes after
// the conversation has moved on. fn returns false when it changed nothing.
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

// sweepLocked drops expired sessions so the map does not grow without bound.
// The caller must hold the mutex.
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
