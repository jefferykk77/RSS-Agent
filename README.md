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
- `models.primary`：首选模型；`models.fallback`：首选故障时才使用的模型。

设置模型环境变量：

```bash
$env:ARK_API_KEY="你的火山方舟 API Key"
$env:ARK_MODEL="你的火山方舟推理接入点 ID"
# 可选：DEEPSEEK_API_KEY 和 DEEPSEEK_MODEL，用作低价熔断回退。
```

运行一次：

```bash
go run ./cmd/rss-agent once
```

## 本地筛选与成本

模型调用前会先在本地去重、排除已读/过期/静默内容，再按优先词排序，并把每次模型候选限制为 24 条，减少不必要的 Token 消耗。规则写在 `profile` 和 `settings`：

```yaml
profile:
  priority_terms: [Eino, AI Agent, Go, LLM]
  muted_feeds: ["低价值来源名称"]
  muted_tags: [招聘, 营销]
settings:
  max_candidates_per_run: 24 # 设为 0 表示不限制
```

命令完成后会输出每类本地跳过数量；进入模型的条目会附带本地命中理由，供模型参考但不强制推送。

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
