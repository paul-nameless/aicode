package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Import the toolCall type from openai.go
type toolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function toolCallFunction `json:"function"`
}

type toolCallFunction struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// GrepToolParams represents the parameters for the GrepTool
type GrepToolParams struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
	Include string `json:"include,omitempty"`
}

// GrepResult represents a single file match result
type GrepResult struct {
	FilePath string    `json:"file_path"`
	Matches  []string  `json:"matches"`
	ModTime  time.Time `json:"-"` // Used for sorting, not exported in JSON
}

// ExecuteGrepTool performs a grep-like search in files
func ExecuteGrepTool(paramsJSON json.RawMessage) (string, error) {
	fmt.Printf("DEBUG - Raw params received: %s\n", string(paramsJSON))

	// Try multiple approaches to handle potential JSON format issues

	// 1. Try direct unmarshaling first
	var params GrepToolParams
	err := json.Unmarshal(paramsJSON, &params)

	// 2. If that fails, try to handle string-encoded JSON
	if err != nil {
		var strArg string
		if err2 := json.Unmarshal(paramsJSON, &strArg); err2 == nil {
			// We got a string, check if it's JSON
			if strings.HasPrefix(strArg, "{") && strings.HasSuffix(strArg, "}") {
				fmt.Printf("DEBUG - Found string-encoded JSON: %s\n", strArg)
				if err3 := json.Unmarshal([]byte(strArg), &params); err3 == nil {
					// Successfully parsed
					fmt.Printf("DEBUG - Successfully parsed string-encoded JSON\n")
				} else {
					// Both approaches failed
					return "", fmt.Errorf("failed to parse grep tool parameters: %v (from string: %v)", err, err3)
				}
			} else {
				// It's a simple string, assume it's just the pattern
				params.Pattern = strArg
				fmt.Printf("DEBUG - Treating as simple pattern: %s\n", strArg)
			}
		} else {
			// Both approaches failed
			return "", fmt.Errorf("failed to parse grep tool parameters: %v", err)
		}
	}

	// Validate parameters
	if params.Pattern == "" {
		return "", fmt.Errorf("pattern parameter is required")
	}

	fmt.Printf("DEBUG - Using pattern: %s, path: %s, include: %s\n",
		params.Pattern, params.Path, params.Include)

	// Default path to current directory if not provided
	if params.Path == "" {
		var err error
		params.Path, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("failed to get current directory: %v", err)
		}
	} else {
		// Ensure path is absolute
		if !filepath.IsAbs(params.Path) {
			absPath, err := filepath.Abs(params.Path)
			if err != nil {
				return "", fmt.Errorf("failed to convert path to absolute: %v", err)
			}
			params.Path = absPath
		}
	}

	// Compile regex pattern with case-insensitive option if not using specific regex syntax
	var pattern *regexp.Regexp
	var compileErr error

	// Check if the pattern already contains regex-specific syntax
	hasRegexSyntax := strings.ContainsAny(params.Pattern, "^$.*+?()[]{}|\\")

	if !hasRegexSyntax {
		// If it's a simple string without regex syntax, make it case-insensitive
		pattern, compileErr = regexp.Compile("(?i)" + regexp.QuoteMeta(params.Pattern))
	} else {
		// It's already a regex pattern, use as is
		pattern, compileErr = regexp.Compile(params.Pattern)
	}

	if compileErr != nil {
		return "", fmt.Errorf("invalid regex pattern: %v", compileErr)
	}

	// Search for files
	results, err := searchFiles(params.Path, pattern, params.Include)
	if err != nil {
		return "", fmt.Errorf("search failed: %v", err)
	}

	// Sort results by modification time (newest first)
	sort.Slice(results, func(i, j int) bool {
		return results[i].ModTime.After(results[j].ModTime)
	})

	// Format the results
	return formatResults(results), nil
}

// searchFiles recursively searches files matching the include pattern for content matching the regex pattern
func searchFiles(rootPath string, pattern *regexp.Regexp, includePattern string) ([]GrepResult, error) {
	var results []GrepResult

	// Ensure rootPath exists
	if _, err := os.Stat(rootPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("path does not exist: %s", rootPath)
	}

	err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip this file/directory but continue walking
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Check if file matches include pattern
		if includePattern != "" {
			// Handle glob patterns like "*.{js,ts}"
			if strings.Contains(includePattern, "{") && strings.Contains(includePattern, "}") {
				// Extract patterns between braces
				startIdx := strings.Index(includePattern, "{")
				endIdx := strings.Index(includePattern, "}")

				if startIdx != -1 && endIdx != -1 && startIdx < endIdx {
					prefix := includePattern[:startIdx]
					patterns := strings.Split(includePattern[startIdx+1:endIdx], ",")
					suffix := includePattern[endIdx+1:]

					matched := false
					for _, p := range patterns {
						fullPattern := prefix + p + suffix
						if m, err := filepath.Match(fullPattern, filepath.Base(path)); err == nil && m {
							matched = true
							break
						}
					}

					if !matched {
						return nil // Skip this file
					}
				}
			} else {
				// Simple pattern matching
				match, err := filepath.Match(includePattern, filepath.Base(path))
				if err != nil {
					return nil // Invalid pattern, skip but continue
				}
				if !match {
					return nil // File doesn't match pattern, skip
				}
			}
		}

		// Skip binary files or very large files
		if info.Size() > 10*1024*1024 { // Skip files larger than 10MB
			return nil
		}

		// Read file content
		content, err := os.ReadFile(path)
		if err != nil {
			return nil // Skip this file but continue walking
		}

		// Convert to string and search for matches
		contentStr := string(content)
		matches := pattern.FindAllString(contentStr, -1)

		if len(matches) > 0 {
			// Deduplicate matches
			uniqueMatches := make(map[string]bool)
			for _, match := range matches {
				uniqueMatches[match] = true
			}

			// Convert back to slice
			matchesSlice := make([]string, 0, len(uniqueMatches))
			for match := range uniqueMatches {
				matchesSlice = append(matchesSlice, match)
			}

			// Add to results
			results = append(results, GrepResult{
				FilePath: path,
				Matches:  matchesSlice,
				ModTime:  info.ModTime(),
			})
		}

		return nil
	})

	return results, err
}

// ToolCallResult represents the result of a tool call
type ToolCallResult struct {
	CallID string
	Output string
}

// HandleToolCalls processes tool calls from the LLM response (original function kept for backward compatibility)
func HandleToolCalls(toolCalls []toolCall) (string, error) {
	response, _, err := HandleToolCallsWithResults(toolCalls)
	return response, err
}

// HandleToolCallsWithResults processes tool calls and returns both formatted response and structured results
func HandleToolCallsWithResults(toolCalls []toolCall) (string, []ToolCallResult, error) {
	var toolResponse strings.Builder
	toolResponse.WriteString("Tool calls detected:\n\n")

	var results []ToolCallResult

	for _, toolCall := range toolCalls {
		toolName := toolCall.Function.Name

		toolResponse.WriteString(fmt.Sprintf("Tool: %s\nArguments: %s\n",
			toolName,
			string(toolCall.Function.Arguments)))

		// Execute the tool based on the name
		var result string
		var err error

		switch toolName {
		case "GrepTool":
			result, err = ExecuteGrepTool(toolCall.Function.Arguments)
			if err != nil {
				result = fmt.Sprintf("Error executing GrepTool: %v", err)
			}
		default:
			// For now, other tools aren't implemented yet
			result = fmt.Sprintf("Tool %s is not implemented yet.", toolName)
		}

		// Store the result for later use in follow-up requests
		results = append(results, ToolCallResult{
			CallID: toolCall.ID,
			Output: result,
		})

		toolResponse.WriteString(fmt.Sprintf("\nResult:\n%s\n\n", result))
	}

	// Print the results to stdout for debugging
	fmt.Println(toolResponse.String())

	return toolResponse.String(), results, nil
}

// formatResults formats the grep results as a string
func formatResults(results []GrepResult) string {
	if len(results) == 0 {
		return "No matches found."
	}

	var sb strings.Builder
	totalMatches := 0
	for _, result := range results {
		totalMatches += len(result.Matches)
	}

	sb.WriteString(fmt.Sprintf("Found %d matches in %d files:\n\n", totalMatches, len(results)))

	// Limit the number of files and matches to display to avoid overwhelming output
	maxFilesToShow := 20
	// maxMatchesPerFile := 5

	for i, result := range results {
		if i >= maxFilesToShow {
			remaining := len(results) - maxFilesToShow
			sb.WriteString(fmt.Sprintf("\n... and %d more files not shown\n", remaining))
			break
		}

		sb.WriteString(fmt.Sprintf("%s\n", result.FilePath))

		// if len(result.Matches) <= maxMatchesPerFile {
		// 	// Show all matches
		// 	sb.WriteString(fmt.Sprintf("Matches: %s\n\n", strings.Join(result.Matches, ", ")))
		// } else {
		// 	// Show limited matches with a count
		// 	matches := result.Matches[:maxMatchesPerFile]
		// 	sb.WriteString(fmt.Sprintf("Matches (%d total): %s, ...\n\n",
		// 		len(result.Matches),
		// 		strings.Join(matches, ", ")))
		// }
	}

	return sb.String()
}
