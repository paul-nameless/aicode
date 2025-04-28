package main

import (
	"fmt"
	"os/user"
	"path/filepath"
	"strings"
)

// formatTokenCount converts token counts to a more readable format
// For counts >= 1000, it displays as X.Xk (e.g., 1500 → 1.5k)
func formatTokenCount(count int) string {
	if count >= 1000 {
		return fmt.Sprintf("%.1fk", float64(count)/1000)
	}
	return fmt.Sprintf("%d", count)
}

// expandHomeDir expands the tilde in the path to the user's home directory
func expandHomeDir(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}

	usr, err := user.Current()
	if err != nil {
		return path // Return original path if we can't get user home
	}

	return filepath.Join(usr.HomeDir, path[1:])
}

// chunkOutput processes a long output string and returns message chunks
// If output is more than maxLines lines, it returns first maxLines + message about remaining
func chunkOutput(output string, maxLines int) []string {
	lines := strings.Split(output, "\n")

	if len(lines) <= maxLines {
		// If fewer than maxLines lines, return as is
		return []string{output}
	}

	// If more than maxLines lines, return first maxLines + info message
	firstChunk := strings.Join(lines[:maxLines], "\n")
	remainingCount := len(lines) - maxLines

	return []string{
		firstChunk,
		fmt.Sprintf("... %d more lines", remainingCount),
	}
}
