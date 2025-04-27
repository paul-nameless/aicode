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

Profiles let you easily switch between AI Code configurations for different workflows. Example use cases include:

- Commit profile: uses a simple model, non-interactive mode, and a custom commit message prompt
- Task-completion profile: fetches descriptions from Jira/Linear, completes tasks, tests, commits, pushes, and updates task status
- Analysis profile: read-only for code review

Profiles are configured as YAML files in the `configs/` directory. Pass `-c <profile>` to select a profile, e.g. `aicode -c commit`.

### Example profile (`configs/commit.yml`):

```yaml
api_key_shell: "pass show example/openai.com-api-key" # Use shell cmd to get the API key, do not store it in the config file
model: "gpt-4.1-nano" # Model name for this profile
initial_prompt: "Create a commit message for the following changes:..."
non_interactive: true # Disable interactive UI
notify_cmd: "notify-send 'AI finished'" # Sent when AI finished and terminal is not in focus
```

## Ideas

- Realtime voice transcription to control AiCode
- At the end, run aicode with another profile which will check results of first run. It can improve the quality.
- Run multiples aicode instances, each in it's own git workspace, with different models. At the end, let them review others work and vote for the best approach. The best one will be shown to the user first and others to choose from.
- Use reasoning model at the beginning to create a plan to do the task. Maybe run separate contexts for each step of the plan, but give plan at the beginning.

## Slash Commands

Interact using slash commands:
- `/help`: Display help information
- `/init`: Generate AI.md file with conventions

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.
