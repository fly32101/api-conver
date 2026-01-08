# api-conver

一个 Go 服务，将 OpenAI `/v1/chat/completions` 与 Anthropic `/v1/messages` 请求转换并转发到任意 OpenAI 兼容服务。

## 功能

- `POST /v1/chat/completions` 直接代理到上游 OpenAI 兼容服务
- `POST /v1/messages` 将 Anthropic 请求转换为 OpenAI 结构后代理到上游，并将响应转换回 Anthropic 结构（含工具调用）
- 其他 `/v1/*` 请求原样代理到上游
- `GET /healthz` 健康检查

## 启动

```powershell
go run .
```

使用 OpenAI 兼容服务 Key：

```powershell
$env:OPENAI_API_KEY="your_key"; go run .
```

使用 .env 文件：

```powershell
Copy-Item .env.example .env
go run .
```

## 配置

- `PORT`：服务端口，默认 `8080`
- `OPENAI_BASE_URL`：上游 OpenAI 兼容服务基础地址，默认 `https://api.openai.com/v1`（支持带或不带 `/v1`）
- `OPENAI_API_KEY`：上游 API Key；为空时透传客户端请求头
- `OPENAI_AUTH_HEADER`：鉴权头名称，默认 `Authorization`
- `OPENAI_AUTH_PREFIX`：鉴权前缀，默认 `Bearer`
- `OPENAI_MODEL_DEFAULT`：默认模型，默认 `tstars2.0`
- 兼容旧配置：`IFLOW_*` 仍可用，但优先使用 `OPENAI_*`

## 说明

- Anthropic `stream=true` 已支持，返回 Anthropic SSE（text/tool_use，`usage` 依赖 OpenAI stream usage）。
- Anthropic `tool_use`/`tool_result` 会转换为 OpenAI `tool_calls`/`tool` 消息。
- OpenAI `tool_calls`/`function_call` 会转换为 Anthropic `tool_use`。
- 非 text 的 content block 会被忽略。
- OpenAI `/v1/chat/completions` 未显式传 `stream` 时，会默认补上 `false`。
