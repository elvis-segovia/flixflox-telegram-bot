// Package config loads and validates the bot's runtime parameters. Everything
// comes from environment variables: the token must never end up written in the
// source code.
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

// Config groups the runtime parameters.
type Config struct {
	BotToken       string `envconfig:"BOT_TOKEN"`
	AllowedUserIDs string `envconfig:"ALLOWED_USER_IDS"`
	DownloadDir    string `envconfig:"DOWNLOAD_DIR" default:"./uploads"`

	// APIEndpoint and FileEndpoint point at the local Bot API (or the official
	// one). Both must contain two %s verbs: token and method/path.
	TelegramAPIEndpoint  string `envconfig:"TELEGRAM_API_ENDPOINT"`
	TelegramFileEndpoint string `envconfig:"TELEGRAM_FILE_ENDPOINT"`

	// TelegramDataDir is the local server's volume as seen from the host, and
	// ServerWorkDir the same folder as seen from inside the container. With
	// both, the file_path returned by getFile translates to a real path.
	TelegramDataDir string `envconfig:"TELEGRAM_DATA_DIR" default:"./telegram-data"`
	ServerWorkDir   string `envconfig:"TELEGRAM_SERVER_WORK_DIR" default:"/var/lib/telegram-bot-api"`

	APIUrl string `envconfig:"FLIXFLOX_API_URL"`

	Debug bool

	// allowedUsers is AllowedUserIDs already parsed. It is built once in Load
	// and read on every update, which is the hot path.
	allowedUsers map[int64]struct{}
}

const envPrefix = "FLIXFLOX"

// Load builds the configuration from the environment and validates it.
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
		"TELEGRAM_API_ENDPOINT":  c.TelegramAPIEndpoint,
		"TELEGRAM_FILE_ENDPOINT": c.TelegramFileEndpoint,
	} {
		if strings.Count(endpoint, "%s") != 2 {
			return fmt.Errorf("%s must contain exactly two %%s (token and path), got %q", name, endpoint)
		}
	}
	if c.DownloadDir == "" {
		return errors.New("DOWNLOAD_DIR cannot be empty")
	}
	return nil
}

// IsAllowed reports whether the user may operate the bot. With an empty list
// anyone can use it: that is why bot.New() refuses to start like that.
func (c *Config) IsAllowed(userID int64) bool {
	if len(c.allowedUsers) == 0 {
		return true
	}
	_, ok := c.allowedUsers[userID]
	return ok
}

// HasAllowList reports whether there is at least one authorized user. It is
// checked at startup: without a list, IsAllowed lets anyone through.
func (c *Config) HasAllowList() bool {
	return len(c.allowedUsers) > 0
}

// parseUserIDs turns "123, 456" into the set of authorized IDs.
//
// An entry that is not a number aborts startup instead of being dropped
// silently: an empty list means "anyone can use the bot", so a mistyped ID
// would leave the bot wide open without anyone noticing.
func parseUserIDs(raw string) (map[int64]struct{}, error) {
	ids := make(map[int64]struct{})

	for part := range strings.SplitSeq(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("ALLOWED_USER_IDS: %q is not a numeric ID", part)
		}
		ids[id] = struct{}{}
	}
	return ids, nil
}
