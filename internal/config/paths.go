package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileURL arma la URL de descarga a partir del file_path que devuelve getFile.
func (c *Config) FileURL(serverFilePath string) string {
	return fmt.Sprintf(c.FileEndpoint, c.BotToken, strings.TrimPrefix(serverFilePath, "/"))
}

// HostPath traduce el file_path que devuelve getFile (una ruta absoluta dentro
// del contenedor, en modo local) a la ruta equivalente en el host.
// Devuelve false si el file_path no cae bajo ServerWorkDir, que es el caso del
// Bot API oficial, donde hay que bajar el archivo por HTTP.
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
