# api-conver

一个基于 DDD 架构的 Go 服务，将 OpenAI `/v1/chat/completions` 与 Anthropic `/v1/messages` 请求转换并转发到任意 OpenAI 兼容服务。支持通过别名配置多个上游服务。

## 功能

- `GET /healthz` - 健康检查
- `POST /v1/chat/completions` - 代理到全局配置的上游
- `POST /v1/responses` - 代理到全局配置的上游
- `POST /v1/messages` - Anthropic 请求转换后代理到全局配置
- `POST /{alias}/v1/chat/completions` - 代理到指定别名的上游
- `POST /{alias}/v1/responses` - 代理到指定别名的上游
- `POST /{alias}/v1/messages` - Anthropic 请求转换后代理到指定别名
- 其他 `/v1/*` 请求原样代理到上游

## 启动

```bash
go run ./cmd/server
```

## 配置

### config.yaml

推荐使用 `config.yaml` 配置：

```yaml
aliases:
  openai:
    base_url: "https://api.openai.com/v1"
    api_key: "sk-xxx"
    auth_header: "Authorization"
    auth_prefix: "Bearer"
    default_model: "gpt-4"

  claude:
    base_url: "https://api.anthropic.com/v1"
    api_key: "sk-ant-xxx"
    auth_header: "x-api-key"
    default_model: "claude-3-5-sonnet-20241022"

defaults:
  port: "8080"
```

### 环境变量

兼容旧的环境变量配置（作为 fallback）：

| 变量 | 说明 |
|------|------|
| `OPENAI_BASE_URL` | 上游基础地址，默认 `https://api.openai.com/v1` |
| `OPENAI_API_KEY` | 上游 API Key，为空时透传客户端认证 |
| `OPENAI_AUTH_HEADER` | 认证头名称，默认 `Authorization` |
| `OPENAI_AUTH_PREFIX` | 认证前缀，默认 `Bearer` |
| `IFLOW_*` | 旧配置，仍可用但优先级较低 |

### 调用示例

```bash
# 使用全局配置
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -d '{"messages": [{"role": "user", "content": "hi"}]}'

# /v1/responses
curl http://localhost:8080/v1/responses \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -d '{"model":"gpt-4.1","input":"Tell me a three sentence bedtime story about a unicorn."}'

# 使用别名路由到指定上游
curl http://localhost:8080/openai/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"messages": [{"role": "user", "content": "hi"}]}'

# Anthropic 协议请求
curl http://localhost:8080/claude/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: $ANTHROPIC_API_KEY" \
  -d '{"messages": [{"role": "user", "content": "hi"}], "max_tokens": 100}'
```

## 说明

- Anthropic `stream=true` 已支持，返回 Anthropic SSE 格式
- Anthropic `tool_use`/`tool_result` 会与 OpenAI `tool_calls` 互相转换
- 非 text 的 content block 会被忽略
- OpenAI 请求未传 `stream` 时，默认补上 `false`

## 架构

```
cmd/server/main.go           # 应用入口
internal/
  config/config.go           # YAML 配置加载
  domain/
    model/                   # 数据模型（OpenAI、Anthropic）
    service/converter.go     # 协议转换逻辑
  application/usecase/       # 用例编排
  infrastructure/
    proxy/client.go          # 上游 HTTP 客户端
    repository/config.go     # 配置仓储
  interface/
    handler/handler.go       # HTTP 处理函数
    router/router.go         # Gin 路由 + 中间件
```
