package bot

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"flixflox-telegram-bot/internal/storage"
)

// downloadOpts ajusta una descarga puntual. El cero vale para el caso normal:
// el archivo se guarda con el nombre que trae y nadie espera el resultado.
type downloadOpts struct {
	// baseName reemplaza el nombre del archivo (sin extensión). Sirve para los
	// adjuntos que tienen un lugar fijo, como la imagen de fondo.
	baseName string

	// onSaved corre al terminar bien, con la ruta final. Corre en la goroutine
	// de la descarga, así que tiene que ser seguro para uso concurrente.
	onSaved func(path string)
}

// startDownload confirma la recepción y baja el archivo en segundo plano, para
// que el bot siga respondiendo mientras se mueven varios GB.
func (b *Bot) startDownload(ctx context.Context, msg *tgbotapi.Message, sess session, opts downloadOpts) {
	media, ok := extractMedia(msg)
	if !ok {
		return
	}

	status := tgbotapi.NewMessage(msg.Chat.ID, "⏳ Saving file...")
	status.ReplyToMessageID = msg.MessageID
	statusMsg := b.deliver(status)

	b.wg.Go(func() {
		path, err := b.saveMedia(ctx, media, sess, opts.baseName)
		var text string
		if err != nil {
			log.Printf("error guardando %s: %v", media.fileID, err)
			text = "❌ Could not save the file: " + err.Error()
		} else {
			text = "✅ Saved to:\n" + path
			if opts.onSaved != nil {
				opts.onSaved(path)
			}
		}

		// Si el mensaje de estado se envió, se edita; si no, se manda uno nuevo.
		if statusMsg != nil {
			b.deliver(tgbotapi.NewEditMessageText(msg.Chat.ID, statusMsg.MessageID, text))
			return
		}
		b.send(msg.Chat.ID, text)
	})
}

// saveMedia resuelve el archivo en el servidor local (o lo baja por HTTP) y lo
// deja en uploads/<categoría>/<título>/. Devuelve la ruta final.
func (b *Bot) saveMedia(ctx context.Context, media mediaFile, sess session, baseName string) (string, error) {
	file, err := b.api.GetFile(tgbotapi.FileConfig{FileID: media.fileID})
	if err != nil {
		return "", fmt.Errorf("getFile: %w", err)
	}

	destDir := filepath.Join(b.cfg.DownloadDir, sess.Category.dir(), sess.Title)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("creando carpeta destino: %w", err)
	}

	// El nombre viene del usuario: se sanea siempre, y si no queda nada usable
	// se cae al nombre generado a partir del file_unique_id.
	name := storage.SanitizeName(baseName)
	if name == "" {
		name = storage.SanitizeName(media.fileName)
	}
	if name == "" {
		name = media.fallbackName
	}
	if ext := filepath.Ext(name); ext == "" {
		name += filepath.Ext(file.FilePath)
	}

	destPath, err := storage.UniquePath(filepath.Join(destDir, name))
	if err != nil {
		return "", err
	}

	if srcPath, ok := b.cfg.HostPath(file.FilePath); ok {
		if _, statErr := os.Stat(srcPath); statErr == nil {
			if err := storage.MoveFile(srcPath, destPath); err != nil {
				return "", fmt.Errorf("moviendo archivo: %w", err)
			}
			return destPath, nil
		}
		log.Printf("file_path %q no existe en el host, se descarga por HTTP", srcPath)
	}

	if err := storage.DownloadFile(ctx, b.cfg.FileURL(file.FilePath), destPath); err != nil {
		return "", fmt.Errorf("descargando archivo: %w", err)
	}
	return destPath, nil
}
