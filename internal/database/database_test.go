package database

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDatabaseOperations(t *testing.T) {
	// Setup: Create a temporary database file
	dbPath := "test.db"
	defer os.Remove(dbPath)

	db, err := NewDatabase(dbPath)
	require.NoError(t, err, "Failed to create database")
	defer db.Close()

	// Test: Create a playlist using the internal method to get the playlist ID
	tx, err := db.Begin()
	require.NoError(t, err, "Failed to begin transaction")

	playlistID, err := db.getOrCreatePlaylist(tx, "test_playlist_id", "Test Playlist")
	require.NoError(t, err, "Failed to create playlist")
	assert.NotZero(t, playlistID, "Playlist ID should not be zero")

	err = tx.Commit()
	require.NoError(t, err, "Failed to commit transaction")

	// Test: Add a video
	metadata := VideoMetadata{
		Title:       "Test Video",
		Description: "Test Description",
		Channel:     "Test Channel",
		ChannelID:   "test_channel_id",
		Duration:    300,
		ViewCount:   1000,
		UploadDate:  time.Now(),
	}

	err = db.AddVideo("test_video_id", fmt.Sprintf("%d", playlistID), "Test Playlist", metadata)
	require.NoError(t, err, "Failed to add video")

	// Manually set file_path to make it eligible for validation
	_, err = db.db.Exec("UPDATE videos SET file_path = 'test_path.mp3' WHERE youtube_id = ?", "test_video_id")
	require.NoError(t, err, "Failed to set file_path")

	// Set last_validated to NULL to ensure it needs validation
	_, err = db.db.Exec("UPDATE videos SET last_validated = NULL WHERE youtube_id = ?", "test_video_id")
	require.NoError(t, err, "Failed to set last_validated to NULL")

	// Test: Check if video exists
	exists, err := db.VideoExists("test_video_id")
	require.NoError(t, err, "Failed to check video existence")
	assert.True(t, exists, "Video should exist in database")

	// Test: Get videos needing validation
	videos, err := db.GetVideosNeedingValidation(24 * time.Hour)
	require.NoError(t, err, "Failed to get videos needing validation")
	assert.NotEmpty(t, videos, "Should find videos needing validation")
	assert.Contains(t, videos, "test_video_id", "Test video should need validation")

	// Test: Run ValidateFiles
	_, err = db.ValidateFiles()
	require.NoError(t, err, "ValidateFiles should not fail")
}
