package main

import (
	_ "embed"
)

//go:embed prompts/system.md
var defaultSystemPrompt string

//go:embed prompts/init.md
var _ string // init prompt is currently unused

//go:embed prompts/summary.md
var summaryPrompt string
