package validator

import (
	"log"
	"os"
	"time"

	"github.com/sampiiiii/pp-downloader/internal/database"
)

type Validator struct {
	db            *database.Database
	outputDir     string
	checkInterval time.Duration
	stopChan      chan struct{}
}

func NewValidator(db *database.Database, outputDir string, checkInterval time.Duration) *Validator {
	return &Validator{
		db:            db,
		outputDir:     outputDir,
		checkInterval: checkInterval,
		stopChan:      make(chan struct{}),
	}
}

// Start begins the periodic validation process
func (v *Validator) Start() {
	// Run first validation immediately
	v.RunValidation()

	// Then run on the specified interval
	ticker := time.NewTicker(v.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			v.RunValidation()
		case <-v.stopChan:
			log.Println("Validation service stopped")
			return
		}
	}
}

// Stop gracefully shuts down the validation service
func (v *Validator) Stop() {
	close(v.stopChan)
}

// RunValidation performs a single validation pass
func (v *Validator) RunValidation() {
	log.Println("Starting file validation...")
	start := time.Now()

	// Get videos that need validation (older than 1 week by default)
	videos, err := v.db.GetVideosNeedingValidation(7 * 24 * time.Hour)
	if err != nil {
		log.Printf("Error getting videos for validation: %v", err)
		return
	}

	if len(videos) == 0 {
		log.Println("No files need validation at this time")
		return
	}

	log.Printf("Validating %d files...", len(videos))
	validated, err := v.db.ValidateFiles()
	if err != nil {
		log.Printf("Error during validation: %v", err)
		return
	}

	log.Printf("Validation completed in %s. %d files validated.",
		time.Since(start).Round(time.Millisecond), validated)
}

// CleanupMissingFiles removes database entries for files that no longer exist
func (v *Validator) CleanupMissingFiles() (int, error) {
	log.Println("Cleaning up missing files...")

	tx, err := v.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	// Get all videos with missing files
	rows, err := tx.Query(`
		SELECT youtube_id, file_path 
		FROM videos 
		WHERE file_path IS NOT NULL 
		  AND validation_status = 'missing'
	`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var deleted int

	for rows.Next() {
		var youtubeID, filePath string
		if err := rows.Scan(&youtubeID, &filePath); err != nil {
			log.Printf("Error scanning row: %v", err)
			continue
		}

		// Double-check the file doesn't exist
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			// File is confirmed missing, delete the record
			_, err := tx.Exec(`
				DELETE FROM videos 
				WHERE youtube_id = ?
			`, youtubeID)
			if err != nil {
				log.Printf("Error deleting record for missing file %s: %v", youtubeID, err)
				continue
			}
			deleted++
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	log.Printf("Cleaned up %d missing files", deleted)
	return deleted, nil
}
