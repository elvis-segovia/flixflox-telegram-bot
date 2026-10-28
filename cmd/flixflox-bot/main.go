// Command flixflox-bot receives movies and TV shows over Telegram and stores
// them tidily on disk.
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

	// Ctrl+C or SIGTERM stops the update receiver and cancels the downloads in
	// flight; each one is given time to clean up its temporary file.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return b.Run(ctx)
}
