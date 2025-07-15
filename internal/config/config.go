package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	MusicParentDir string            `mapstructure:"MUSIC_PARENT_DIR"`
	FFmpegPath     string            `mapstructure:"FFMPEG_PATH"`
	JSONPath       string            `mapstructure:"JSON_PATH"`
	DBPath         string            `mapstructure:"DB_PATH"`
	WatchInterval  time.Duration     `mapstructure:"WATCH_INTERVAL"`
	Playlists      map[string]string `json:"playlists"`
}

func LoadConfig(path string) (*Config, error) {
	// Load environment variables from .env file if it exists
	viper.SetConfigFile(filepath.Join(path, ".env"))
	viper.AutomaticEnv()

	// Read .env file
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(*os.PathError); !ok {
			return nil, err
		}
	}

	// Load JSON config
	configPath := viper.GetString("JSON_PATH")
	if configPath == "" {
		configPath = "/config/playlists.json" // Default path in container
	}

	jsonData, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var config Config
	if err := json.Unmarshal(jsonData, &config); err != nil {
		return nil, err
	}

	// Bind environment variables
	// Set environment variables explicitly
	config.MusicParentDir = viper.GetString("MUSIC_PARENT_DIR")
	config.FFmpegPath = viper.GetString("FFMPEG_PATH")
	config.JSONPath = viper.GetString("JSON_PATH")
	config.DBPath = viper.GetString("DB_PATH")

	// Parse watch interval
	if watchInterval := viper.GetString("WATCH_INTERVAL"); watchInterval != "" {
		if duration, err := time.ParseDuration(watchInterval); err == nil {
			config.WatchInterval = duration
		}
	}

	// Set defaults if not specified
	if config.MusicParentDir == "" {
		config.MusicParentDir = "/music"
	}
	if config.FFmpegPath == "" {
		config.FFmpegPath = "/usr/bin/ffmpeg"
	}
	if config.JSONPath == "" {
		config.JSONPath = "/config/playlists.json"
	}
	if config.DBPath == "" {
		config.DBPath = "/music/downloads.db"
	}

	// Set default watch interval if not specified
	if config.WatchInterval == 0 {
		config.WatchInterval = 15 * time.Minute // Default to 15 minutes
	}

	return &config, nil
}
