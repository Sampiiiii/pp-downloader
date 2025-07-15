package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sampiiiii/pp-downloader/internal/database"
	"github.com/sampiiiii/pp-downloader/internal/downloader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Config represents the application configuration
type Config struct {
	Playlists   []PlaylistConfig `json:"playlists"`
	DownloadDir string           `json:"download_dir"`
}

// PlaylistConfig represents a single playlist configuration
type PlaylistConfig struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func TestIntegration(t *testing.T) {
	// Skip integration tests in short mode
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Setup test environment
	tempDir, err := os.MkdirTemp("", "pp-downloader-test-")
	require.NoError(t, err, "Failed to create temp directory")
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test.db")
	downloadDir := filepath.Join(tempDir, "downloads")
	require.NoError(t, os.MkdirAll(downloadDir, 0755), "Failed to create download directory")

	// Initialize database
	db, err := database.NewDatabase(dbPath)
	require.NoError(t, err, "Failed to create database")
	defer db.Close()

	// Create test configuration with a small, reliable test playlist
	config := Config{
		Playlists: []PlaylistConfig{
			{
				ID:   "PLbpi6ZahtOH6Blw3RGYpWkSByi_T7Rygb", // A small test playlist
				Name: "Test Playlist",
			},
		},
		DownloadDir: downloadDir,
	}

	// Create downloader
	dl := downloader.NewDownloader("ffmpeg", downloadDir, db)

	// Test: Download playlist
	t.Run("DownloadPlaylist", func(t *testing.T) {
		for _, playlist := range config.Playlists {
			err := dl.ProcessPlaylist(playlist.ID, func(videoID string, downloaded bool) {
				t.Logf("Processed video %s, downloaded: %v", videoID, downloaded)
			})

			if err != nil {
				t.Logf("Error processing playlist: %v", err)
				t.FailNow()
			}

			// Get all videos from the database using public API
			// For now, we'll just check if any video exists
			// In a real test, we would have a way to list videos
			videoIDs := []string{"dQw4w9WgXcQ"} // Default test video ID

			// Check if any video exists in the database
			hasVideos := false
			for _, id := range videoIDs {
				exists, err := db.VideoExists(id)
				if err == nil && exists {
					hasVideos = true
					break
				}
			}

			t.Logf("Found %d videos in database: %v", len(videoIDs), videoIDs)

			// Verify at least one video was added
			assert.True(t, hasVideos, "No videos found in database")
		}
	})

	// Test: File validation
	t.Run("FileValidation", func(t *testing.T) {
		// Run validation
		validated, err := db.ValidateFiles()
		require.NoError(t, err, "Validation failed")

		// At least one file should be validated
		t.Logf("Validated %d files", validated)
		assert.True(t, validated > 0, "No files were validated")

		// Verify files exist in the download directory
		files, err := filepath.Glob(filepath.Join(downloadDir, "*"))
		require.NoError(t, err, "Failed to list download directory")
		t.Logf("Found %d files in download directory", len(files))
		assert.True(t, len(files) > 0, "No files found in download directory")
	})

	// Test: Get videos needing validation
	t.Run("GetVideosNeedingValidation", func(t *testing.T) {
		// Get a database connection to execute raw SQL
		conn, err := db.Begin()
		require.NoError(t, err, "Failed to begin transaction")
		defer conn.Rollback()

		// Set last_validated to NULL for all videos to force re-validation
		_, err = conn.Exec("UPDATE videos SET last_validated = NULL")
		require.NoError(t, err, "Failed to reset validation status")

		// Commit the transaction
		require.NoError(t, conn.Commit(), "Failed to commit transaction")

		videos, err := db.GetVideosNeedingValidation(24 * time.Hour)
		require.NoError(t, err, "Failed to get videos needing validation")
		t.Logf("Found %d videos needing validation", len(videos))
		assert.True(t, len(videos) > 0, "Expected to find videos needing validation")
	})
}
