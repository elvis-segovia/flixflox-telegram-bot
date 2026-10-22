// Command flixflox-bot recibe películas y series por Telegram y las guarda
// ordenadas en disco.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"flixflox-telegram-bot/internal/bot"
	"flixflox-telegram-bot/internal/config"
)

func main() {
	log.SetFlags(log.LstdFlags)
	log.SetPrefix("flixflox: ")

	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	clientHttp := &http.Client{
		Timeout: 30 * time.Second,
	}

	b, err := bot.New(cfg, clientHttp)
	if err != nil {
		return err
	}

	// Ctrl+C o SIGTERM cortan el receptor de updates y cancelan las descargas
	// en curso; se espera a que cada una limpie su archivo temporal.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return b.Run(ctx)
}
