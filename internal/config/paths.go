package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileURL builds the download URL from the file_path returned by getFile.
func (c *Config) FileURL(serverFilePath string) string {
	return fmt.Sprintf(c.TelegramFileEndpoint, c.BotToken, strings.TrimPrefix(serverFilePath, "/"))
}

// HostPath translates the file_path returned by getFile (an absolute path
// inside the container, in local mode) into the equivalent path on the host.
// It returns false when the file_path does not fall under ServerWorkDir, which
// is the case with the official Bot API, where the file must be fetched over
// HTTP.
func (c *Config) HostPath(serverFilePath string) (string, bool) {
	if serverFilePath == "" || !filepath.IsAbs(serverFilePath) {
		return "", false
	}

	rel, err := filepath.Rel(c.ServerWorkDir, serverFilePath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", false
	}
	return filepath.Join(c.TelegramDataDir, rel), true
}
