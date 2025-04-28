# AI

## Build/Run Commands
- Build: `go build .`
- Run with OpenAI: `OPENAI_API_KEY=<key> ./aicode`
- Run with Anthropic: `ANTHROPIC_API_KEY=<key> ./aicode`
- Run with prompt: `./aicode -q "your prompt"`
- Run with profile: `./aicode -p review`
- Format and lint: `task ch`
- Format only: `task fmt` or `go fmt .`
- Lint only: `task lint`
- Docker build: `docker build -t aicode .`
- Docker run: `docker run --rm -it -v $PWD:/app -e OPENAI_API_KEY=<key> aicode`

## Code Style Guidelines
- Formatting: Run `go fmt .` before committing
- Error handling: Always check errors and provide meaningful messages
- Function design: Small, focused functions with explicit return types
- Code: Split code into logical units/functions, each with a clear purpose
- Variables: camelCase for variables, PascalCase for exported identifiers
- Imports: Group standard library, third-party, and local packages
- Comments: Minimize code comments unless absolutely necessary
- Commits: Small, focused changes with concise commit messages
- Dependencies: Use `go mod vendor` for dependency management
