# AiCode

AI-powered CLI tool for software engineering tasks.

## Features

- Interactive CLI interface with AI assistance
- Seamless integration with your local development environment
- File search, view, and edit capabilities
- Support for multiple AI models (OpenAI, Anthropic)
- Persistent memory for project context via CLAUDE.md

## Installation

### Using Go

```bash
go install github.com/paul-nameless/aicode@latest
```

### Using Docker

```bash
docker build -t aicode .
docker run --rm -it -v $PWD:/app -e OPENAI_API_KEY=your_api_key aicode
```

### Manual Installation

```bash
git clone https://github.com/paul-nameless/aicode.git
cd aicode
go build .
```

## Configuration

AiCode requires an API key from OpenAI or Anthropic:

### OpenAI

```bash
export OPENAI_API_KEY=your_api_key
aicode
```

### Anthropic

```bash
export ANTHROPIC_API_KEY=your_api_key
aicode
```

## Usage

### Basic Usage

```bash
# Start interactive session
aicode

# Run with a specific prompt
aicode -q "find all TODO comments in the codebase"
```

## Profiles

I wanted an app to have different profiles/config. There are multiople use cases, for example:

- profile for commit, weak model, non interactive and custom prompt
- separate profile which can get description of the task from jira/linear, complete the task, run tests, commit, push the changes and move task to in-review
- profile for analysis, read only
-

## Ideas

- realtime
-

## Slash Commands

Interact using slash commands:
- `/help`: Display help information
- `/init`: Generate AI.md file with conventions

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.
