# Repository Guidelines

## Project Structure & Layout

- Root files: `main.go` (entry), `go.mod`, `go.sum`, `Makefile`, `docker-compose.yml`, `README.md`.
- Runtime logs are written to `logs/` as daily `access-YYYY-MM-DD.log` and `error-YYYY-MM-DD.log`.
- Build artifacts are generated into `build/` by `Makefile` targets; do not commit this directory.

## Build, Run & Development

- Local run (no container): `go run main.go` (listens on `:8080`).
- Local build: `go build -o google-proxy .` or use `make build` with `GOOS/GOARCH` exported.
- Cross compilation: `make linux-amd64`, `make darwin-arm64`, etc. Outputs to `build/google-proxy-*`.
- Containers: `docker-compose up -d` for the recommended setup, or `docker build -t google-proxy .` then `docker run --rm -p 8080:8080 google-proxy`.

## Coding Style & Naming Conventions

- Language: Go modules, Go version as declared in `go.mod`.
- Always format with `gofmt` (or `go fmt ./...`) before committing.
- Follow idiomatic Go naming: `lowerCamelCase` for unexported, `UpperCamelCase` for exported. Keep package-level constants and vars grouped.
- Prefer standard library and existing dependencies (`godotenv`, `x/net/proxy`); avoid adding new deps unless clearly justified.
- Keep comments and user-facing docs in Simplified Chinese when possible, consistent with `README.md`.

## Testing Guidelines

- Use the standard `testing` package and `net/http/httptest` for handlers and middleware.
- Place tests in `_test.go` files next to the code, with names like `TestNewReverseProxy` and `TestIPRateLimiter`.
- Run tests with `go test ./...` before opening a pull request. Add focused tests for new rate limiting, logging, or proxy behavior.

## Commit & Pull Request Guidelines

- Commit messages: short, imperative, English, e.g. `add ip rate limit metrics`, `refactor logging setup`.
- One logical change per commit where practical; keep diffs small and reviewable.
- Pull requests should include: purpose and high-level changes, how you tested (commands used and results), and any behavior or config impact (ports, env vars like `SOCKS5_URL`).
- Avoid committing secrets in `.env`, logs, or Docker-related files; treat `SOCKS5_URL` as sensitive configuration.

