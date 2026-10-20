package bot

import (
	"log"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// send manda texto plano a propósito: las rutas y títulos vienen del usuario y
// con ParseMode un carácter suelto rompe el mensaje entero.
func (b *Bot) send(chatID int64, text string) *tgbotapi.Message {
	return b.deliver(tgbotapi.NewMessage(chatID, text))
}

// deliver envía cualquier Chattable y loguea el error en vez de ignorarlo.
func (b *Bot) deliver(c tgbotapi.Chattable) *tgbotapi.Message {
	sent, err := b.api.Send(c)
	if err != nil {
		log.Printf("error enviando mensaje: %v", err)
		return nil
	}
	return &sent
}

func (b *Bot) answerCallback(callbackID, text string) {
	if _, err := b.api.Request(tgbotapi.NewCallback(callbackID, text)); err != nil {
		// Un callback vencido (más de ~1 minuto) siempre falla; no es grave.
		if !strings.Contains(err.Error(), "query is too old") {
			log.Printf("error respondiendo callback: %v", err)
		}
	}
}
