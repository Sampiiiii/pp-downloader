package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// VideoMetadata represents metadata for a downloaded video
type VideoMetadata struct {
	Title         string    `json:"title"`
	Description   string    `json:"description"`
	Channel       string    `json:"channel"`
	ChannelID     string    `json:"channel_id"`
	Duration      int       `json:"duration"`
	ViewCount     int64     `json:"view_count"`
	ThumbnailURL  string    `json:"thumbnail_url,omitempty"`
	UploadDate    time.Time `json:"upload_date,omitempty"`
	IsLive        bool      `json:"is_live,omitempty"`
	LiveStartTime time.Time `json:"live_start_time,omitempty"`
	LiveEndTime   time.Time `json:"live_end_time,omitempty"`
	MetadataJSON  string    `json:"metadata_json,omitempty"`
}

// Playlist represents a YouTube playlist in the database
type Playlist struct {
	ID          int64          `json:"id"`
	YoutubeID   string         `json:"youtube_id"`
	Title       string         `json:"title"`
	Description sql.NullString `json:"description,omitempty"`
	Thumbnail   sql.NullString `json:"thumbnail,omitempty"`
	Channel     sql.NullString `json:"channel,omitempty"`
	ChannelID   sql.NullString `json:"channel_id,omitempty"`
	VideoCount  int            `json:"video_count"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	LastChecked time.Time      `json:"last_checked"`
}

type Database struct {
	db *sql.DB
}

// Begin starts a new transaction
func (d *Database) Begin() (*sql.Tx, error) {
	return d.db.Begin()
}

// GetOrCreatePlaylist gets an existing playlist or creates a new one
func (d *Database) GetOrCreatePlaylist(youtubeID, title string) (*Playlist, error) {
	tx, err := d.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	var playlist Playlist

	err = tx.QueryRow("SELECT id, youtube_id, title, description, thumbnail, channel, channel_id, video_count, last_checked, created_at, updated_at FROM playlists WHERE youtube_id = ?", youtubeID).Scan(
		&playlist.ID,
		&playlist.YoutubeID,
		&playlist.Title,
		&playlist.Description,
		&playlist.Thumbnail,
		&playlist.Channel,
		&playlist.ChannelID,
		&playlist.VideoCount,
		&playlist.LastChecked,
		&playlist.CreatedAt,
		&playlist.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			// Create a new playlist
			result, err := tx.Exec(`
				INSERT INTO playlists (youtube_id, title, description, thumbnail, channel, channel_id, created_at, updated_at, last_checked)
				VALUES (?, ?, NULL, NULL, NULL, NULL, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
			`, youtubeID, title)
			if err != nil {
				return nil, fmt.Errorf("failed to insert playlist: %w", err)
			}
			id, err := result.LastInsertId()
			if err != nil {
				return nil, fmt.Errorf("failed to get last insert id: %w", err)
			}
			playlist.ID = id
			playlist.YoutubeID = youtubeID
			playlist.Title = title
			playlist.Description = sql.NullString{String: "", Valid: false}
			playlist.Thumbnail = sql.NullString{String: "", Valid: false}
			playlist.Channel = sql.NullString{String: "", Valid: false}
			playlist.ChannelID = sql.NullString{String: "", Valid: false}
			playlist.CreatedAt = time.Now()
			playlist.UpdatedAt = time.Now()
			playlist.LastChecked = time.Now()
		} else {
			return nil, fmt.Errorf("failed to query playlist: %w", err)
		}
	}

	// No need to set these fields as they are already set during the scan

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return &playlist, nil
}

// VideoExists checks if a video exists in the database
func (d *Database) VideoExists(youtubeID string) (bool, error) {
	var exists bool
	err := d.db.QueryRow("SELECT EXISTS(SELECT 1 FROM videos WHERE youtube_id = ?)", youtubeID).Scan(&exists)
	return exists, err
}

// NewDatabase initializes a new database connection and ensures the schema exists
func NewDatabase(dbPath string) (*Database, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	// Create tables if they don't exist
	if err := createSchema(db); err != nil {
		return nil, fmt.Errorf("failed to create schema: %w", err)
	}

	return &Database{db: db}, nil
}

// Close closes the database connection
func (d *Database) Close() error {
	return d.db.Close()
}

// UpdateFileInfo updates the file information for a downloaded video
func (d *Database) UpdateFileInfo(youtubeID, filePath string, fileSize int64) error {
	_, err := d.db.Exec(
		`UPDATE videos 
		SET file_path = ?, 
		    file_size = ?,
		    validation_status = 'valid',
		    last_validated = CURRENT_TIMESTAMP,
		    updated_at = CURRENT_TIMESTAMP
		WHERE youtube_id = ?`,
		filePath,
		fileSize,
		youtubeID,
	)
	return err
}

// ValidateFiles checks the existence of all downloaded files and updates their status
// Returns the number of files checked and any error encountered
func (d *Database) ValidateFiles() (int, error) {
	tx, err := d.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Get all videos with file paths
	rows, err := tx.Query(`
		SELECT youtube_id, file_path 
		FROM videos 
		WHERE file_path IS NOT NULL 
		  AND file_path != ''
	`)
	if err != nil {
		return 0, fmt.Errorf("failed to query videos: %w", err)
	}
	defer rows.Close()

	var checked, missing int
	now := time.Now().UTC().Format(time.RFC3339)

	for rows.Next() {
		var youtubeID, filePath string
		if err := rows.Scan(&youtubeID, &filePath); err != nil {
			log.Printf("Error scanning video row: %v", err)
			continue
		}

		checked++
		_, err := os.Stat(filePath)
		status := "valid"
		if os.IsNotExist(err) {
			status = "missing"
			missing++
		} else if err != nil {
			status = "error"
			log.Printf("Error checking file %s: %v", filePath, err)
		}

		_, err = tx.Exec(
			`UPDATE videos 
			SET validation_status = ?,
			    last_validated = ?,
			    updated_at = ?
			WHERE youtube_id = ?`,
			status,
			now,
			now,
			youtubeID,
		)
		if err != nil {
			log.Printf("Error updating validation status for %s: %v", youtubeID, err)
		}
	}

	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("error iterating rows: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Printf("Validated %d files, %d missing", checked, missing)
	return checked, nil
}

// GetVideosNeedingValidation returns videos that need to be validated
// maxAge is the maximum age of the last validation (e.g., 7*24*time.Hour for weekly)
func (d *Database) GetVideosNeedingValidation(maxAge time.Duration) ([]string, error) {
	var ids []string
	
	rows, err := d.db.Query(`
		SELECT youtube_id 
		FROM videos 
		WHERE file_path IS NOT NULL 
		  AND file_path != ''
		  AND (last_validated IS NULL 
		       OR last_validated < datetime('now', ?))
	`, fmt.Sprintf("-%d seconds", int(maxAge.Seconds())))
	
	if err != nil {
		return nil, fmt.Errorf("failed to query videos needing validation: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("error scanning row: %w", err)
		}
		ids = append(ids, id)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return ids, nil
}

// createSchema creates the necessary database tables
func createSchema(db *sql.DB) error {
	schemas := []string{
		`CREATE TABLE IF NOT EXISTS playlists (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			youtube_id TEXT NOT NULL UNIQUE,
			title TEXT NOT NULL,
			description TEXT,
			thumbnail TEXT,
			channel TEXT,
			channel_id TEXT,
			video_count INTEGER DEFAULT 0,
			last_checked TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS videos (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			youtube_id TEXT NOT NULL UNIQUE,
			playlist_id INTEGER NOT NULL,
			playlist_title TEXT NOT NULL,
			title TEXT NOT NULL,
			description TEXT,
			channel TEXT NOT NULL,
			channel_id TEXT,
			duration INTEGER NOT NULL DEFAULT 0,
			view_count INTEGER DEFAULT 0,
			thumbnail_url TEXT,
			upload_date TIMESTAMP,
			is_live BOOLEAN DEFAULT FALSE,
			live_start_time TIMESTAMP,
			live_end_time TIMESTAMP,
			metadata_json TEXT,
			file_path TEXT,  -- Path to the downloaded file
			file_size INTEGER DEFAULT 0,  -- File size in bytes
			file_checksum TEXT,  -- Optional: MD5/SHA1 checksum of the file
			last_validated TIMESTAMP,  -- When the file was last validated
			validation_status TEXT DEFAULT 'pending',  -- 'valid', 'missing', 'corrupt'
			downloaded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (playlist_id) REFERENCES playlists(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_videos_youtube_id ON videos(youtube_id);`,
		`CREATE INDEX IF NOT EXISTS idx_videos_playlist_id ON videos(playlist_id);`,
		`CREATE INDEX IF NOT EXISTS idx_videos_upload_date ON videos(upload_date);`,
	}

	for _, schema := range schemas {
		if _, err := db.Exec(schema); err != nil {
			return fmt.Errorf("failed to execute schema: %w", err)
		}
	}

	return nil
}

// IsVideoDownloaded checks if a video has already been downloaded
func (d *Database) IsVideoDownloaded(youtubeID string) (bool, error) {
	var exists bool
	err := d.db.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM videos WHERE youtube_id = ?)",
		youtubeID,
	).Scan(&exists)

	return exists, err
}

// AddVideo adds a video to the database with metadata
func (d *Database) AddVideo(youtubeID, playlistYoutubeID, playlistTitle string, metadata VideoMetadata) error {
	// Generate a unique file path based on video title and ID
	safeTitle := sanitizeFilename(metadata.Title)
	filePath := fmt.Sprintf(".music/%s [%s].mp3", safeTitle, youtubeID)
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// First, get or create the playlist to ensure it exists and get its ID
	playlist, err := d.GetOrCreatePlaylist(playlistYoutubeID, playlistTitle)
	if err != nil {
		return fmt.Errorf("failed to get or create playlist: %w", err)
	}

	// Insert or update video
	_, err = tx.Exec(`
		INSERT INTO videos (
			youtube_id, playlist_id, playlist_title, title, description, 
			channel, channel_id, duration, view_count, 
			thumbnail_url, upload_date, is_live, 
			live_start_time, live_end_time, metadata_json,
			file_path, file_size, validation_status, last_validated
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(youtube_id) DO UPDATE SET
			playlist_id = excluded.playlist_id,
			playlist_title = excluded.playlist_title,
			title = excluded.title,
			description = excluded.description,
			channel = excluded.channel,
			channel_id = excluded.channel_id,
			duration = excluded.duration,
			view_count = excluded.view_count,
			thumbnail_url = excluded.thumbnail_url,
			upload_date = excluded.upload_date,
			is_live = excluded.is_live,
			live_start_time = excluded.live_start_time,
			live_end_time = excluded.live_end_time,
			metadata_json = excluded.metadata_json,
			file_path = excluded.file_path,
			file_size = excluded.file_size,
			validation_status = excluded.validation_status,
			last_validated = excluded.last_validated,
			updated_at = CURRENT_TIMESTAMP
	`,
		youtubeID, playlist.ID, playlistTitle, metadata.Title, metadata.Description,
		metadata.Channel, metadata.ChannelID, metadata.Duration, metadata.ViewCount,
		metadata.ThumbnailURL, metadata.UploadDate, metadata.IsLive,
		metadata.LiveStartTime, metadata.LiveEndTime, metadata.MetadataJSON,
		filePath, 0, "pending", time.Now().UTC(),
	)

	if err != nil {
		return fmt.Errorf("failed to insert/update video: %w", err)
	}

	// Update playlist last_checked and video count
	_, err = tx.Exec(
		`UPDATE playlists 
		SET last_checked = ?, 
		    updated_at = CURRENT_TIMESTAMP,
		    video_count = (SELECT COUNT(*) FROM videos WHERE playlist_id = ?)
		WHERE id = ?`,
		time.Now().UTC(),
		playlist.ID,
		playlist.ID,
	)
	if err != nil {
		return fmt.Errorf("failed to update playlist: %w", err)
	}

	return tx.Commit()
}

// getOrCreatePlaylist gets an existing playlist or creates a new one
func (d *Database) getOrCreatePlaylist(tx *sql.Tx, youtubeID, title string) (int64, error) {
	// Try to get existing playlist
	var id int64
	var existingTitle string

	err := tx.QueryRow(
		"SELECT id, title FROM playlists WHERE youtube_id = ?", 
		youtubeID,
	).Scan(&id, &existingTitle)

	if err == nil {
		// Playlist exists, update its title if needed
		if existingTitle != title {
			_, err = tx.Exec(`
				UPDATE playlists 
				SET title = ?, updated_at = CURRENT_TIMESTAMP
				WHERE id = ?
			`, title, id)
			if err != nil {
				return 0, fmt.Errorf("failed to update playlist title: %w", err)
			}
		}
	} else if err != sql.ErrNoRows {
		return 0, fmt.Errorf("failed to query playlist: %w", err)
	}

	// Create new playlist
	result, err := tx.Exec(
		`INSERT INTO playlists (
			youtube_id, 
			title,
			created_at,
			updated_at
		) VALUES (?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		youtubeID,
		title,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to create playlist: %w", err)
	}

	id, err = result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get last insert ID: %w", err)
	}

	return id, nil
}

// GetLastChecked returns the last time the playlist was checked
func (d *Database) GetLastChecked(playlistYoutubeID string) (time.Time, error) {
	var lastChecked time.Time
	err := d.db.QueryRow(
		"SELECT last_checked FROM playlists WHERE youtube_id = ?",
		playlistYoutubeID,
	).Scan(&lastChecked)

	if err == sql.ErrNoRows {
		return time.Time{}, nil
	} else if err != nil {
		return time.Time{}, fmt.Errorf("failed to get last checked time: %w", err)
	}

	return lastChecked, nil
}

// sanitizeFilename removes invalid characters from filenames
func sanitizeFilename(filename string) string {
	// Remove invalid characters
	re := regexp.MustCompile(`[<>:"/\\|?*]`)
	sanitized := re.ReplaceAllString(filename, "")
	
	// Replace multiple spaces with single space
	sanitized = regexp.MustCompile(`\s+`).ReplaceAllString(sanitized, " ")
	
	// Trim spaces
	return strings.TrimSpace(sanitized)
}
