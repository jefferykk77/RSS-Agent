# RSS Agent

一个用 Go + Eino 做的 RSS Agent：订阅 RSS，按你的兴趣筛选，调用 LLM 总结，只推值得看的内容。

## 快速开始

```bash
go run ./cmd/rss-agent init
```

编辑 `config.yaml`：

- `profile.interests`：你在意什么。
- `profile.exclude`：你不想看的内容。
- `feeds`：RSS 订阅源。
- `model.name`：可写死模型名，也可用 `${OPENAI_MODEL}`。

设置模型环境变量：

```bash
$env:OPENAI_API_KEY="你的 key"
$env:OPENAI_MODEL="你的模型名"
# 可选：$env:OPENAI_BASE_URL="https://..."
```

运行一次：

```bash
go run ./cmd/rss-agent once
```

定时运行：

```bash
go run ./cmd/rss-agent watch
```

## 订阅源管理

```bash
go run ./cmd/rss-agent add "CloudWeGo Blog" "https://www.cloudwego.io/feed.xml" -tag go -tag eino
go run ./cmd/rss-agent list
```

## 推送

默认会输出到控制台。如果设置 `RSS_AGENT_WEBHOOK_URL`，会同时 POST 一份 JSON：

```json
{
  "text": "Markdown 摘要",
  "items": []
}
```

可以接到你自己的飞书/钉钉/Slack 转发服务。

## Eino 使用点

核心筛选和总结逻辑在 `internal/agent`，使用 `github.com/cloudwego/eino-ext/components/model/openai` 创建 Eino `ChatModel`，再通过 `schema.Message` 把 RSS 条目批量交给模型评分、筛选和总结。

