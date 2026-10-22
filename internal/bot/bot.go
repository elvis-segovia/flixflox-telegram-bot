// Package bot conecta con el Bot API, recibe los updates y los despacha.
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

// Bot es el estado compartido del bot: configuración, cliente del API y las
// sesiones en curso.
type Bot struct {
	cfg        *config.Config
	api        *tgbotapi.BotAPI
	sessions   *sessionStore
	httpClient *http.Client

	// La lista de géneros la define el API y casi no cambia, así que se cachea
	// en vez de pedirla en cada carga.
	genresMu sync.Mutex
	genres   []string
	genresAt time.Time

	// wg espera a las descargas en curso cuando el bot se apaga.
	wg sync.WaitGroup
}

// New abre la conexión con el Bot API y deja el bot listo para recibir updates.
func New(cfg *config.Config, httpClient *http.Client) (*Bot, error) {
	api, err := tgbotapi.NewBotAPIWithAPIEndpoint(cfg.BotToken, cfg.TelegramAPIEndpoint)
	if err != nil {
		return nil, err
	}
	api.Debug = cfg.Debug

	// Sin lista de autorizados cualquiera puede mandarle archivos al bot, así
	// que no se arranca: se devuelve el error en vez de matar el proceso desde
	// una función que ya reporta errores.
	if !cfg.HasAllowList() {
		return nil, errors.New("ALLOWED_USER_IDS está vacío: cualquiera podría usar el bot")
	}

	log.Printf("Connect as @%s", api.Self.UserName)

	return &Bot{cfg: cfg, api: api, sessions: newSessionStore(), httpClient: httpClient}, nil
}

// Run procesa updates hasta que ctx se cancela. Al cortar se espera a que cada
// descarga en curso limpie su archivo temporal.
func (b *Bot) Run(ctx context.Context) error {
	if err := os.MkdirAll(b.cfg.DownloadDir, 0o755); err != nil {
		return err
	}

	updateCfg := tgbotapi.NewUpdate(0)
	updateCfg.Timeout = 60
	updates := b.api.GetUpdatesChan(updateCfg)

	go func() {
		<-ctx.Done()
		log.Print("apagando, cancelando descargas en curso...")
		b.api.StopReceivingUpdates()
	}()

	for update := range updates {
		b.handleUpdate(ctx, update)
	}

	b.wg.Wait()
	log.Print("listo")
	return nil
}
