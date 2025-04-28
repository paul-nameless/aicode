package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"time"
)

// LogFile holds the reference to the open log file
var LogFile *os.File

const (
	MaxLogSize = 10 * 1024 * 1024 // 10MB default max log size
)

// InitLogger initializes the application logger
func InitLogger(debug bool) {
	// Create logs directory in user's data directory if it doesn't exist
	usr, err := user.Current()
	if err != nil {
		panic(err)
	}
	logDir := filepath.Join(usr.HomeDir, ".local", "share", "aicode")
	err = os.MkdirAll(logDir, 0755)
	if err != nil {
		panic(err)
	}

	logPath := filepath.Join(logDir, "aicode.log")

	// Check if log needs truncation
	TruncateLogIfNeeded(logPath, MaxLogSize)

	LogFile, err = os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		panic(err)
	}

	// Set up the handler with appropriate log level based on debug flag
	logLevel := slog.LevelInfo
	if debug {
		logLevel = slog.LevelDebug
	}

	handler := slog.NewTextHandler(LogFile, &slog.HandlerOptions{
		Level: logLevel,
	})

	logger := slog.New(handler)
	slog.SetDefault(logger)
	slog.Info("AiCode started", "version", "0.1")
}

// TruncateLogIfNeeded checks if the log file exceeds maxSize and truncates it if needed
// It keeps the most recent portion of the log and adds a truncation message
func TruncateLogIfNeeded(logPath string, maxSize int64) {
	// Check if log file exists
	fileInfo, err := os.Stat(logPath)
	if os.IsNotExist(err) {
		return // File doesn't exist yet, nothing to truncate
	} else if err != nil {
		fmt.Fprintf(os.Stderr, "Error checking log file size: %v\n", err)
		return
	}

	// Check if size exceeds threshold
	if fileInfo.Size() <= maxSize {
		return // No need to truncate
	}

	// Open log file for reading
	originalFile, err := os.Open(logPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening log file for truncation: %v\n", err)
		return
	}
	defer originalFile.Close()

	// Create a temporary file for the truncated logs
	tempFile, err := os.CreateTemp(filepath.Dir(logPath), "aicode-log-truncated")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating temp file for log truncation: %v\n", err)
		return
	}
	defer tempFile.Close()

	// Calculate how much of the file to keep (half of the max size)
	keepSize := maxSize / 2

	// Seek to the position to start keeping logs
	_, err = originalFile.Seek(fileInfo.Size()-keepSize, io.SeekStart)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error seeking in log file: %v\n", err)
		return
	}

	// Write truncation message to the temp file
	truncationMsg := fmt.Sprintf("\n--- Log truncated at %s (original size: %.2f MB) ---\n\n",
		time.Now().Format(time.RFC3339),
		float64(fileInfo.Size())/float64(1024*1024))
	_, err = tempFile.WriteString(truncationMsg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error writing truncation message: %v\n", err)
		return
	}

	// Copy remaining content to temp file
	_, err = io.Copy(tempFile, originalFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error copying log data: %v\n", err)
		return
	}

	// Close both files before rename operation
	originalFile.Close()
	tempFile.Close()

	// Replace original log with truncated version
	err = os.Rename(tempFile.Name(), logPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error replacing log file after truncation: %v\n", err)
		return
	}
}
