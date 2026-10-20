// Package config carga y valida los parámetros de ejecución del bot. Todo
// viene de variables de entorno: el token jamás debe quedar escrito en el
// código fuente.
package config

import (
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
)

// Config agrupa los parámetros de ejecución.
type Config struct {
	BotToken       string `envconfig:"BOT_TOKEN"`
	AllowedUserIDs string `envconfig:"ALLOWED_USER_IDS"`
	DownloadDir    string `envconfig:"DOWNLOAD_DIR" default:"./uploads"`

	// APIEndpoint y FileEndpoint apuntan al Bot API local (o al oficial).
	// Ambos deben contener dos verbos %s: token y método/ruta.
	APIEndpoint  string `envconfig:"TELEGRAM_API_ENDPOINT"`
	FileEndpoint string `envconfig:"TELEGRAM_FILE_ENDPOINT"`

	// TelegramDataDir es el volumen del servidor local visto desde el host,
	// y ServerWorkDir la misma carpeta vista desde adentro del contenedor.
	// Con los dos se traduce el file_path que devuelve getFile a una ruta real.
	TelegramDataDir string `envconfig:"TELEGRAM_DATA_DIR" default:"./telegram-data"`
	ServerWorkDir   string `envconfig:"TELEGRAM_SERVER_WORK_DIR" default:"/var/lib/telegram-bot-api"`

	Debug bool

	// allowedUsers es AllowedUserIDs ya parseado. Se arma una sola vez en Load
	// y se consulta en cada update, que es la ruta caliente.
	allowedUsers map[int64]struct{}
}

const envPrefix = "FLIXFLOX"

// Load arma la configuración desde el entorno y la valida.
func Load() (*Config, error) {
	var cfg Config

	if err := godotenv.Load(); err != nil {
		log.Printf("No .env file found or failed to load: %v", err)
	}

	if err := envconfig.Process(envPrefix, &cfg); err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	token := strings.TrimSpace(cfg.BotToken)
	if token == "" {
		return nil, errors.New("BOT_TOKEN is empty")
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	allowedUsers, err := parseUserIDs(cfg.AllowedUserIDs)
	if err != nil {
		return nil, err
	}
	cfg.allowedUsers = allowedUsers

	return &cfg, nil
}

func (c *Config) validate() error {
	for name, endpoint := range map[string]string{
		"TELEGRAM_API_ENDPOINT":  c.APIEndpoint,
		"TELEGRAM_FILE_ENDPOINT": c.FileEndpoint,
	} {
		if strings.Count(endpoint, "%s") != 2 {
			return fmt.Errorf("%s debe contener exactamente dos %%s (token y ruta), se recibió %q", name, endpoint)
		}
	}
	if c.DownloadDir == "" {
		return errors.New("DOWNLOAD_DIR no puede estar vacío")
	}
	return nil
}

// IsAllowed indica si el usuario puede operar el bot. Con la lista vacía
// cualquiera puede usarlo: por eso bot.New() se niega a arrancar así.
func (c *Config) IsAllowed(userID int64) bool {
	if len(c.allowedUsers) == 0 {
		return true
	}
	_, ok := c.allowedUsers[userID]
	return ok
}

// HasAllowList indica si hay al menos un usuario autorizado. Se chequea al
// arrancar: sin lista, IsAllowed deja pasar a cualquiera.
func (c *Config) HasAllowList() bool {
	return len(c.allowedUsers) > 0
}

// parseUserIDs convierte "123, 456" en el conjunto de IDs autorizados.
//
// Un elemento que no es un número corta el arranque en vez de descartarse en
// silencio: la lista vacía significa "cualquiera puede usar el bot", así que
// un ID mal escrito dejaría el bot abierto sin que nadie se entere.
func parseUserIDs(raw string) (map[int64]struct{}, error) {
	ids := make(map[int64]struct{})

	for part := range strings.SplitSeq(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("ALLOWED_USER_IDS: %q no es un ID numérico", part)
		}
		ids[id] = struct{}{}
	}
	return ids, nil
}
