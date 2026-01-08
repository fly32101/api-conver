# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Run locally
go run ./cmd/server

# Build for current platform
go build -o server ./cmd/server
```

## Configuration

Using `config.yaml` (with `IFLOW_*`/`OPENAI_*` env vars as fallback):

```yaml
aliases:
  openai:
    base_url: "https://api.openai.com/v1"
    api_key: "sk-xxx"
    default_model: "gpt-4"
  claude:
    base_url: "https://api.anthropic.com/v1"
    api_key: "sk-ant-xxx"
    default_model: "claude-3-5-sonnet"

defaults:
  port: "8080"
```

## Architecture

DDD + Gin framework structure:

```
cmd/server/main.go           # Application entrypoint
internal/
  config/config.go           # YAML config loading
  domain/
    model/                   # Data models (OpenAI, Anthropic)
    service/converter.go     # Protocol conversion logic
  application/usecase/       # Use case orchestration
  infrastructure/
    proxy/client.go          # Upstream HTTP client
    repository/config.go     # Config repository
  interface/
    handler/handler.go       # HTTP handlers
    router/router.go         # Gin routes + middleware
```

**Routes:**
- `GET /healthz` - Health check
- `GET /{alias}/healthz` - Alias-specific health check
- `POST /v1/chat/completions` - Legacy route (uses global config)
- `POST /{alias}/v1/chat/completions` - Route by alias to upstream
- `POST /v1/messages` - Legacy Anthropic route
- `POST /{alias}/v1/messages` - Alias-specific Anthropic route
- `POST /v1/*` and `POST /{alias}/v1/*` - Passthrough proxy
