package bot

import (
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// mediaFile normaliza los distintos tipos de adjunto de Telegram.
type mediaFile struct {
	fileID       string
	fileName     string
	fallbackName string
}

func extractMedia(msg *tgbotapi.Message) (mediaFile, bool) {
	switch {
	case msg.Document != nil: // se chequea primero: archivos sin comprimir
		return mediaFile{
			fileID:       msg.Document.FileID,
			fileName:     msg.Document.FileName,
			fallbackName: "file_" + msg.Document.FileUniqueID,
		}, true

	case msg.Video != nil:
		return mediaFile{
			fileID:       msg.Video.FileID,
			fileName:     msg.Video.FileName,
			fallbackName: "video_" + msg.Video.FileUniqueID + ".mp4",
		}, true

	case msg.Audio != nil:
		return mediaFile{
			fileID:       msg.Audio.FileID,
			fileName:     msg.Audio.FileName,
			fallbackName: "audio_" + msg.Audio.FileUniqueID + ".mp3",
		}, true

	case len(msg.Photo) > 0:
		// La última entrada es la de mayor resolución.
		photo := msg.Photo[len(msg.Photo)-1]
		return mediaFile{
			fileID:       photo.FileID,
			fallbackName: "photo_" + photo.FileUniqueID + ".jpg",
		}, true

	default:
		return mediaFile{}, false
	}
}

func hasMedia(msg *tgbotapi.Message) bool {
	_, ok := extractMedia(msg)
	return ok
}
