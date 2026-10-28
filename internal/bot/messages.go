package bot

import (
	"log"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// send deliberately sends plain text: paths and titles come from the user, and
// with a ParseMode a single stray character breaks the whole message.
func (b *Bot) send(chatID int64, text string) *tgbotapi.Message {
	return b.deliver(tgbotapi.NewMessage(chatID, text))
}

// deliver sends any Chattable and logs the error instead of ignoring it.
func (b *Bot) deliver(c tgbotapi.Chattable) *tgbotapi.Message {
	sent, err := b.api.Send(c)
	if err != nil {
		log.Printf("error sending message: %v", err)
		return nil
	}
	return &sent
}

func (b *Bot) answerCallback(callbackID, text string) {
	if _, err := b.api.Request(tgbotapi.NewCallback(callbackID, text)); err != nil {
		// An expired callback (more than ~1 minute) always fails; not serious.
		if !strings.Contains(err.Error(), "query is too old") {
			log.Printf("error answering callback: %v", err)
		}
	}
}
