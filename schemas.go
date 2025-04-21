package main

import (
	_ "embed"
)

//go:embed tools/view.md
var ViewToolDescription string

//go:embed tools/replace.md
var ReplaceToolDescription string

//go:embed tools/edit.md
var EditToolDescription string

//go:embed tools/bash.md
var BashToolDescription string

//go:embed tools/ls.md
var LsToolDescription string

//go:embed tools/find_files.md
var FindFilesDescription string

//go:embed tools/dispatch_agent.md
var DispatchAgentDescription string

//go:embed tools/fetch.md
var FetchToolDescription string

//go:embed tools/grep.md
var GrepDescription string

//go:embed tools/view.json
var ViewToolSchema string

//go:embed tools/replace.json
var ReplaceToolSchema string

//go:embed tools/edit.json
var EditToolSchema string

//go:embed tools/bash.json
var BashToolSchema string

//go:embed tools/ls.json
var LsToolSchema string

//go:embed tools/find_files.json
var FindFilesSchema string

//go:embed tools/dispatch_agent.json
var DispatchAgentSchema string

//go:embed tools/fetch.json
var FetchToolSchema string

//go:embed tools/grep.json
var GrepSchema string
