package bot

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"flixflox-telegram-bot/internal/storage"
)

type kind string

const (
	background kind = "background"
	video      kind = "video"
)

// downloadOpts tunes a single download. The zero value covers the normal case:
// the file is saved under the name it comes with and nobody waits for the
// result.
type downloadOpts struct {
	baseName string
	kind     kind
	onSaved  func(path string)
}

// startDownload acknowledges the file and fetches it in the background, so the
// bot keeps answering while several GB are being moved.
func (b *Bot) startDownload(ctx context.Context, msg *tgbotapi.Message, sess session, opts downloadOpts) {
	media, ok := extractMedia(msg)
	if !ok {
		return
	}

	status := tgbotapi.NewMessage(msg.Chat.ID, "⏳ Saving file...")
	status.ReplyToMessageID = msg.MessageID
	statusMsg := b.deliver(status)

	b.wg.Go(func() {
		path, err := b.saveMedia(ctx, media, sess, opts.baseName, opts.kind)
		var text string
		if err != nil {
			log.Printf("error saving %s: %v", media.fileID, err)
			text = "❌ Could not save the file: " + err.Error()
		} else {
			text = "✅ Saved to:\n" + path
			if opts.onSaved != nil {
				opts.onSaved(path)
			}
		}

		// If the status message went out it is edited; otherwise a new one is sent.
		if statusMsg != nil {
			b.deliver(tgbotapi.NewEditMessageText(msg.Chat.ID, statusMsg.MessageID, text))
			return
		}
		b.send(msg.Chat.ID, text)
	})
}

func (b *Bot) saveMedia(ctx context.Context, media mediaFile, sess session, baseName string, kind kind) (string, error) {
	file, err := b.api.GetFile(tgbotapi.FileConfig{FileID: media.fileID})
	if err != nil {
		return "", fmt.Errorf("getFile: %w", err)
	}

	folderName := SanitizeFilename(sess.Title)

	// Only episodes are stored under S01/E01: a movie has no season number, and
	// demanding one anyway left the file unsaved.
	destDir := filepath.Join(b.cfg.DownloadDir, sess.Category.dir(), folderName)
	if kind == video && sess.Category == categoryTVShow {
		season, errSeason := strconv.Atoi(sess.SeasonNumber)
		episode, errEpisode := strconv.Atoi(sess.EpisodeNumber)

		if errSeason != nil || errEpisode != nil {
			return "", errors.New("Season or episode number are invalid")
		}

		destDir = filepath.Join(destDir, fmt.Sprintf("S%02d", season), fmt.Sprintf("E%02d", episode))
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("Error creating destination folder: %w", err)
	}

	name := SanitizeFilename(baseName)
	if name == "" {
		name = SanitizeFilename(media.fileName)
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
				return "", fmt.Errorf("moving file: %w", err)
			}
			return destPath, nil
		}
		log.Printf("file_path %q does not exist on the host, downloading over HTTP", srcPath)
	}

	if err := storage.DownloadFile(ctx, b.cfg.FileURL(file.FilePath), destPath); err != nil {
		return "", fmt.Errorf("downloading file: %w", err)
	}
	return destPath, nil
}

func SanitizeFilename(name string) string {
	replacer := strings.NewReplacer(
		" ", "_", "/", "_", "\\", "_", ":", "_",
		"*", "_", "?", "_", "\"", "_", "<", "_",
		">", "_", "|", "_", ".", "_",
	)
	return strings.ToLower(replacer.Replace(name))
}
