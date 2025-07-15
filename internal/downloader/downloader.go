package downloader

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	youtube "github.com/kkdai/youtube/v2"
	"github.com/sampiiiii/pp-downloader/internal/database"
)

// VideoInfo represents information about a YouTube video
type VideoInfo struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	Description   string    `json:"description"`
	Duration      float64   `json:"duration"`
	Channel       string    `json:"channel"`
	ChannelID     string    `json:"channel_id"`
	PlaylistID    string    `json:"playlist_id,omitempty"`
	ViewCount     int64     `json:"view_count"`
	Thumbnail     string    `json:"thumbnail"`
	UploadDate    string    `json:"upload_date"`
	LiveStartTime time.Time `json:"live_start_time,omitempty"`
	LiveEndTime   time.Time `json:"live_end_time,omitempty"`
	MetadataJSON  string    `json:"metadata_json,omitempty"`
}

type Downloader struct {
	client     *youtube.Client
	ffmpegPath string
	outputDir  string
	db         *database.Database
}

func NewDownloader(ffmpegPath, outputDir string, db *database.Database) *Downloader {
	return &Downloader{
		client:     &youtube.Client{},
		ffmpegPath: ffmpegPath,
		outputDir:  outputDir,
		db:         db,
	}
}

// ProcessPlaylist downloads all videos from a playlist that haven't been downloaded before
func (d *Downloader) ProcessPlaylist(playlistURL string, playlistName string, callback func(videoID string, downloaded bool)) error {
	// Extract playlist ID from URL
	playlistID := extractPlaylistID(playlistURL)
	if playlistID == "" {
		return fmt.Errorf("invalid playlist URL: %s", playlistURL)
	}

	playlist, err := d.db.GetOrCreatePlaylist(playlistID, playlistName)
	if err != nil {
		return fmt.Errorf("failed to get or create playlist: %w", err)
	}

	log.Printf("Processing playlist '%s' (%s)", playlistName, playlistID)

	// Get all videos in the playlist
	videos, err := d.getPlaylistVideos(playlistURL)
	if err != nil {
		return fmt.Errorf("failed to get playlist videos: %w", err)
	}

	if len(videos) == 0 {
		log.Printf("No videos found in playlist %s", playlistID)
		return nil
	}

	log.Printf("Found %d videos in playlist %s", len(videos), playlistID)

	// Process each video
	for _, video := range videos {
		// Check if video already exists in the database
		exists, err := d.db.VideoExists(video.ID)
		if err != nil {
			log.Printf("Error checking if video %s exists: %v", video.ID, err)
			continue
		}

		if exists {
			log.Printf("Skipping video %s as it already exists in the database", video.ID)
			if callback != nil {
				callback(video.ID, false)
			}
			continue
		}

		// Download the video
		filePath, fileSize, err := d.downloadVideo(video.ID, playlistName) // Pass the friendly name
		if err != nil {
			log.Printf("Failed to download video %s: %v", video.ID, err)
			continue
		}

		// Parse upload date
		var uploadDate time.Time
		if video.UploadDate != "" {
			uploadDate, _ = time.Parse("20060102", video.UploadDate)
		}

		// Prepare video metadata
		metadata := database.VideoMetadata{
			Title:         video.Title,
			Description:   video.Description,
			Channel:       video.Channel,
			ChannelID:     video.ChannelID,
			Duration:      int(video.Duration),
			ViewCount:     video.ViewCount,
			ThumbnailURL:  video.Thumbnail,
			UploadDate:    uploadDate,
			LiveStartTime: video.LiveStartTime,
			LiveEndTime:   video.LiveEndTime,
			MetadataJSON:  video.MetadataJSON,
		}

		// Add video to database
		if err := d.db.AddVideo(video.ID, playlist.YoutubeID, playlist.Title, metadata); err != nil {
			log.Printf("Failed to add video %s to database: %v", video.ID, err)
			continue
		}

		// Update file information
		if err := d.db.UpdateFileInfo(video.ID, filePath, fileSize); err != nil {
			log.Printf("Failed to update file info for video %s: %v", video.ID, err)
		}

		if callback != nil {
			callback(video.ID, true)
		}
	}

	return nil
}

// PlaylistResponse represents the JSON structure returned by yt-dlp for a playlist
// getPlaylistVideos uses yt-dlp to fetch all videos in a playlist
func (d *Downloader) getPlaylistVideos(playlistURL string) ([]VideoInfo, error) {
	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Run yt-dlp to get playlist info as JSON
	cmd := exec.CommandContext(ctx, "yt-dlp",
		"--flat-playlist",
		"--dump-single-json",
		"--no-warnings",
		"--skip-download",
		playlistURL,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("yt-dlp failed: %w\nOutput: %s", err, string(output))
	}

	// Parse the JSON output
	var result struct {
		Entries []VideoInfo `json:"entries"`
	}

	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("failed to parse yt-dlp output: %w", err)
	}

	// Extract playlist ID from URL
	playlistID := extractPlaylistID(playlistURL)

	// Process each video in the playlist
	var videos []VideoInfo
	for _, entry := range result.Entries {
		if entry.ID == "" {
			continue
		}

		// Ensure we have the playlist ID set
		entry.PlaylistID = playlistID
		videos = append(videos, entry)
	}

	return videos, nil
}

// downloadVideo downloads a single video and converts it to mp3
// Returns the output file path, file size in bytes, and any error
func (d *Downloader) downloadVideo(videoID string, playlistName string) (string, int64, error) {
	log.Printf("Downloading video: %s for playlist: %s", videoID, playlistName)

	// Create playlist-specific directory using the playlist name
	playlistDir := filepath.Join(d.outputDir, playlistName)
	if err := os.MkdirAll(playlistDir, 0755); err != nil {
		return "", 0, fmt.Errorf("failed to create playlist directory: %w", err)
	}

	// Create a template for the output filename
	tmpl := filepath.Join(playlistDir, "%(title)s [%(id)s].%(ext)s")
	log.Printf("Using output template: %s", tmpl)
	
	// Use yt-dlp to download the best audio quality and convert to mp3
	cmd := exec.Command("yt-dlp",
		"--extract-audio",
		"--audio-format", "mp3",
		"--audio-quality", "0", // Best quality
		"--embed-thumbnail",
		"--add-metadata",
		"--output", tmpl,
		"--no-warnings",
		"--no-playlist", // Ensure we only download the video, not the whole playlist
		"https://youtube.com/watch?v="+videoID,
	)

	// Add more detailed logging for the command
	log.Printf("Executing yt-dlp command: %v", cmd.Args)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Create a buffer to capture command output
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	if err := cmd.Run(); err != nil {
		return "", 0, fmt.Errorf("yt-dlp download failed: %w\nOutput: %s", err, output.String())
	}

	// Log the output for debugging
	log.Printf("Download output for %s in %s: %s", videoID, playlistName, output.String())

	// Parse the output to find the actual file path
	outputStr := output.String()
	filePath := ""
	for _, line := range strings.Split(outputStr, "\n") {
		if strings.Contains(line, "[ExtractAudio] Destination:") {
			filePath = strings.TrimSpace(strings.Split(line, ":")[1])
		} else if strings.Contains(line, "[download] Destination:") {
			// Fallback for non-audio conversion downloads
			filePath = strings.TrimSpace(strings.Split(line, ":")[1])
		}
	}

	if filePath == "" {
		return "", 0, fmt.Errorf("could not find file path in yt-dlp output")
	}

	// Get file size
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return "", 0, fmt.Errorf("failed to get file size for '%s': %w", filePath, err)
	}

	return filePath, fileInfo.Size(), nil
}

// extractPlaylistID extracts the playlist ID from a YouTube URL
func extractPlaylistID(url string) string {
	// Handle direct ID
	if !strings.Contains(url, "youtube.com") && !strings.Contains(url, "youtu.be") {
		return url
	}

	// Extract from URL parameters
	if strings.Contains(url, "list=") {
		parts := strings.Split(url, "list=")
		if len(parts) > 1 {
			id := strings.Split(parts[1], "&")[0]
			if id != "" {
				return id
			}
		}
	}
	return url
}

func sanitizeFilename(filename string) string {
	// Remove invalid characters
	replacer := strings.NewReplacer(
		"<", "", ">", "", ":", "",
		"\"", "", "/", "", "\\", "",
		"|", "", "?", "", "*", "",
		" ", "_",
	)
	return replacer.Replace(filename)
}
