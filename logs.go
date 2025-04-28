package main

import (
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
)

// LogFile holds the reference to the open log file
var LogFile *os.File

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