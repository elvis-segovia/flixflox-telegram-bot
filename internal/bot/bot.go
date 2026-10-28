// Package bot connects to the Bot API, receives the updates and dispatches
// them.
package bot

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"flixflox-telegram-bot/internal/config"
)

// Bot is the bot's shared state: configuration, API client and the uploads in
// progress.
type Bot struct {
	cfg        *config.Config
	api        *tgbotapi.BotAPI
	sessions   *sessionStore
	httpClient *http.Client

	// The genre list is defined by the API and hardly ever changes, so it is
	// cached instead of being fetched on every upload.
	genresMu sync.Mutex
	genres   []string
	genresAt time.Time

	// wg waits for the downloads in flight when the bot shuts down.
	wg sync.WaitGroup
}

// New opens the connection with the Bot API and leaves the bot ready to
// receive updates.
func New(cfg *config.Config, httpClient *http.Client) (*Bot, error) {
	api, err := tgbotapi.NewBotAPIWithAPIEndpoint(cfg.BotToken, cfg.TelegramAPIEndpoint)
	if err != nil {
		return nil, err
	}
	api.Debug = cfg.Debug

	// Without an allow list anyone could send files to the bot, so it does not
	// start: the error is returned instead of killing the process from a
	// function that already reports errors.
	if !cfg.HasAllowList() {
		return nil, errors.New("ALLOWED_USER_IDS is empty: anyone could use the bot")
	}

	log.Printf("Connect as @%s", api.Self.UserName)

	return &Bot{cfg: cfg, api: api, sessions: newSessionStore(), httpClient: httpClient}, nil
}

// Run processes updates until ctx is cancelled. On shutdown it waits for each
// download in flight to clean up its temporary file.
func (b *Bot) Run(ctx context.Context) error {
	if err := os.MkdirAll(b.cfg.DownloadDir, 0o755); err != nil {
		return err
	}

	updateCfg := tgbotapi.NewUpdate(0)
	updateCfg.Timeout = 60
	updates := b.api.GetUpdatesChan(updateCfg)

	go func() {
		<-ctx.Done()
		log.Print("shutting down, cancelling the downloads in flight...")
		b.api.StopReceivingUpdates()
	}()

	for update := range updates {
		b.handleUpdate(ctx, update)
	}

	b.wg.Wait()
	log.Print("done")
	return nil
}
