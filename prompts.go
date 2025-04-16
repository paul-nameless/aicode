package main

import (
	_ "embed"
)

//go:embed prompts/system.md
var defaultSystemPrompt string

//go:embed prompts/init.md
var initPrompt string
