package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/sampiiiii/pp-downloader/internal/config"
	"github.com/sampiiiii/pp-downloader/internal/database"
	"github.com/sampiiiii/pp-downloader/internal/downloader"
)

// playlistState tracks the state of each playlist for adaptive polling
type playlistState struct {
	lastChecked time.Time
	lastChange  time.Time
	interval    time.Duration
	mu          sync.Mutex
}

// calculateInterval determines the polling interval based on playlist activity
func (ps *playlistState) calculateInterval() time.Duration {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	now := time.Now()
	// If we've seen changes recently, poll more frequently
	if now.Sub(ps.lastChange) < time.Hour*24 {
		return time.Minute * 5 // Check every 5 minutes for active playlists
	}
	return time.Minute * 15 // Default to 15 minutes for less active playlists
}

// updateState updates the playlist state after a check
func (ps *playlistState) updateState(changed bool) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	now := time.Now()
	ps.lastChecked = now
	if changed {
		ps.lastChange = now
	}
}

func main() {
	// Set up logging
	logFile, err := os.OpenFile("pp-downloader.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Printf("Failed to open log file: %v", err)
	} else {
		defer logFile.Close()
		log.SetOutput(io.MultiWriter(os.Stdout, logFile))
	}

	log.Println("Starting Plex Playlist Downloader...")

	// Load configuration
	cfg, err := config.LoadConfig(".")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	log.Printf("Configuration loaded: %+v", cfg)

	// Set default DB path if not specified
	if cfg.DBPath == "" {
		cfg.DBPath = "/config/downloads.db"
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0755); err != nil {
		log.Fatalf("Failed to create database directory: %v", err)
	}

	// Initialize database
	db, err := database.NewDatabase(cfg.DBPath)
	if err != nil {
		log.Fatalf("Error initializing database: %v", err)
	}
	defer db.Close()

	// Ensure music directory exists
	if err := os.MkdirAll(cfg.MusicParentDir, 0755); err != nil {
		log.Fatalf("Error creating music directory: %v", err)
	}

	// Create downloader
	dl := downloader.NewDownloader(cfg.FFmpegPath, cfg.MusicParentDir, db)

	// Initialize playlist states
	playlistStates := make(map[string]*playlistState)
	for name, url := range cfg.Playlists {
		playlistStates[url] = &playlistState{
			interval: time.Minute * 5, // Start with 5 minute intervals
		}
		log.Printf("Watching playlist: %s (%s)", name, url)
	}

	// Handle graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		runScheduler(ctx, cfg, dl, playlistStates)
	}()

	log.Println("Plex Playlist Downloader started. Press Ctrl+C to stop.")

	// Wait for shutdown signal
	<-sigCh
	log.Println("Shutting down...")
	cancel()   // Signal tasks to stop
	wg.Wait()  // Wait for scheduler to finish
	log.Println("Shutdown complete.")
}

// runScheduler manages the scheduling of playlist checks
func runScheduler(ctx context.Context, cfg *config.Config, dl *downloader.Downloader, states map[string]*playlistState) {
	// Initial processing
	processAllPlaylists(ctx, cfg, dl, states, true)

	// Create a ticker for the scheduler (runs every minute)
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Scheduler stopped")
			return
		case <-ticker.C:
			processAllPlaylists(ctx, cfg, dl, states, false)
		}
	}
}

// processAllPlaylists processes all playlists, either immediately or based on their schedule
func processAllPlaylists(ctx context.Context, cfg *config.Config, dl *downloader.Downloader, states map[string]*playlistState, force bool) {
	var wg sync.WaitGroup
	now := time.Now()

	for name, url := range cfg.Playlists {
		state, exists := states[url]
		if !exists {
			state = &playlistState{
				interval: time.Minute * 5, // Default interval
			}
			states[url] = state
		}

		// Check if it's time to process this playlist
		if force || now.Sub(state.lastChecked) >= state.calculateInterval() {
			wg.Add(1)
			go func(name, url string, s *playlistState) {
				defer wg.Done()
				processPlaylist(ctx, dl, name, url, s)
			}(name, url, state)
		}
	}

	// Don't wait for the initial processing to complete
	// wg.Wait()
}

// processPlaylist processes a single playlist and updates its state
func processPlaylist(ctx context.Context, dl *downloader.Downloader, name, url string, state *playlistState) {
	log.Printf("Processing playlist: %s (%s)", name, url)

	// Track if we made any changes
	changed := false

	// Process the playlist
	err := dl.ProcessPlaylist(url, name, func(videoID string, downloaded bool) {
		if downloaded {
			changed = true
			log.Printf("Downloaded new video from %s: %s", name, videoID)
		}
	})

	if err != nil {
		log.Printf("Error processing playlist %s: %v", name, err)
	}

	// Update the playlist state
	state.updateState(changed)

	if changed {
		log.Printf("Playlist %s was updated with new videos", name)
	}
}

func extractPlaylistID(url string) (string, error) {
	// Extract playlist ID from URL
	parts := strings.Split(url, "list=")
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid playlist URL: %s", url)
	}

	// Handle case where there are query parameters after the playlist ID
	playlistID := parts[1]
	if ampIdx := strings.Index(playlistID, "&"); ampIdx != -1 {
		playlistID = playlistID[:ampIdx]
	}

	return playlistID, nil
}
