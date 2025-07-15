package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/sampiiiii-dev/pp-downloader/internal/database"
	"github.com/sampiiiii-dev/pp-downloader/internal/downloader"
)

func main() {
	// Parse command line arguments
	playlistURL := flag.String("playlist", "", "YouTube playlist URL")
	dbPath := flag.String("db", "test.db", "Path to SQLite database")
	outputDir := flag.String("output", "./downloads", "Output directory for downloads")
	flag.Parse()

	if *playlistURL == "" {
		log.Fatal("Please provide a YouTube playlist URL with -playlist")
	}

	// Create output directory if it doesn't exist
	if err := os.MkdirAll(*outputDir, 0755); err != nil {
		log.Fatalf("Failed to create output directory: %v", err)
	}

	// Initialize database
	db, err := database.NewDatabase(*dbPath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// Create downloader
	dl := downloader.NewDownloader("ffmpeg", *outputDir, db)

	// Process playlist
	log.Printf("Processing playlist: %s\n", *playlistURL)
	start := time.Now()

	err = dl.ProcessPlaylist(*playlistURL, func(videoID string, downloaded bool) {
		if downloaded {
			log.Printf("Processed video %s (downloaded: %v)", videoID, downloaded)
		} else {
			log.Printf("Skipped video %s (already downloaded)", videoID)
		}
	})

	if err != nil {
		log.Fatalf("Error processing playlist: %v", err)
	}

	log.Printf("Finished processing playlist in %s\n", time.Since(start))

	// Print database stats
	var videoCount int
	err = db.DB.QueryRow("SELECT COUNT(*) FROM videos").Scan(&videoCount)
	if err != nil {
		log.Printf("Error getting video count: %v", err)
	} else {
		log.Printf("Total videos in database: %d", videoCount)
	}

	// Print some video info
	rows, err := db.DB.Query(`
		SELECT v.youtube_id, v.title, v.channel, v.duration, v.view_count, v.downloaded_at
		FROM videos v
		JOIN playlists p ON v.playlist_id = p.id
		WHERE p.youtube_id = ?
		ORDER BY v.downloaded_at DESC
		LIMIT 5
	`, extractPlaylistID(*playlistURL))

	if err != nil {
		log.Printf("Error querying videos: %v", err)
		return
	}
	defer rows.Close()

	fmt.Println("\nRecently processed videos:")
	fmt.Println("--------------------------------------------------")
	for rows.Next() {
		var (
			youtubeID    string
			title        string
			channel      string
			duration     int
			viewCount    int64
			downloadedAt time.Time
		)

		if err := rows.Scan(&youtubeID, &title, &channel, &duration, &viewCount, &downloadedAt); err != nil {
			log.Printf("Error scanning video row: %v", err)
			continue
		}

		fmt.Printf("Title: %s\n", title)
		fmt.Printf("Channel: %s\n", channel)
		fmt.Printf("Duration: %d seconds\n", duration)
		fmt.Printf("Views: %d\n", viewCount)
		fmt.Printf("Downloaded at: %s\n", downloadedAt.Format(time.RFC3339))
		fmt.Println("--------------------------------------------------")
	}
}

// extractPlaylistID extracts the playlist ID from a YouTube URL
func extractPlaylistID(url string) string {
	// This is a simplified version - you might want to use a proper URL parser
	// or regular expression for production use
	start := "list="
	idx := 0
	for i, c := range url {
		if url[i:i+len(start)] == start {
			idx = i + len(start)
			break
		}
	}
	if idx == 0 {
		return ""
	}

	// Find the end of the playlist ID
	end := idx
	for end < len(url) && url[end] != '&' && url[end] != '#' && url[end] != '?' {
		end++
	}

	return url[idx:end]
}
