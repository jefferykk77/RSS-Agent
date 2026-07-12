# RSS Agent

一个本地优先的 Go + Eino RSS Agent：抓取订阅源，先在本地筛选与去重，再调用你自己的 LLM 做评分和摘要，只推值得看的内容。

项目使用 SQLite 保存状态、抓取缓存、分析结果和成本记录；模型通过 OpenAI 兼容接口接入，默认示例为火山方舟，可配置回退模型。

## 快速开始

生成本地配置：

```powershell
go run ./cmd/rss-agent init
```

这会创建 `config.yaml`。该文件是本地运行配置，已被 Git 忽略；可参考仓库内的 `config.example.yaml`。

在 `config.yaml` 中至少检查：

- `profile`：兴趣、排除项、优先词和静默来源。
- `feeds`：订阅源及其标签。
- `models.primary`：首选模型；`models.fallback`：首选模型调用失败时的回退模型。
- `database.path`：SQLite 数据库位置，默认是 `.rss-agent/rss-agent.db`。
- `settings.full_text_min_chars` 与 `settings.full_text_max_chars`：RSS 内容过短时抓取文章页正文的阈值和最大长度。

设置模型环境变量。火山方舟通常将已授权模型的推理接入点 ID 作为 `ARK_MODEL`：

```powershell
$env:ARK_API_KEY="你的火山方舟 API Key"
$env:ARK_MODEL="你的火山方舟推理接入点 ID"

# 可选：配置 DeepSeek 作为回退模型。
$env:DEEPSEEK_API_KEY="你的 DeepSeek API Key"
$env:DEEPSEEK_MODEL="你的 DeepSeek 模型名"
```

先用一次只读运行验证完整链路：

```powershell
go run ./cmd/rss-agent once -dry-run
```

`-dry-run` 不会写入 SQLite、更新抓取状态或发送 webhook，但候选条目仍会调用 LLM，因此会消耗模型侧额度。确认结果后执行常规运行：

```powershell
go run ./cmd/rss-agent once
```

## 运行与状态

常规 `once` 和 `watch` 运行会在 `database.path` 指定的 SQLite 文件中保存：

- RSS 源的 `ETag`、`Last-Modified` 和抓取状态。后续请求会使用条件请求，`304 Not Modified` 时不会重复解析内容。
- RSS 条目、内容去重信息、已读状态和历史推送记录。
- LLM 评分、摘要、理由和标签。相同文章在相同偏好配置下会命中 `analysis_cache_ttl` 分析缓存，避免重复调用模型。
- 持久化评分任务及其优先级、重试时间和错误状态。应用重启后会恢复未完成任务。
- 模型 token 用量和按模型价格换算的成本记录。

若 RSS 提供的摘要和正文少于 `full_text_min_chars`（默认 600），Agent 会仅为通过本地筛选的候选文章抓取链接页的 HTML 正文。提取并清洗后的正文最多保留 `full_text_max_chars`（默认 8000）个字符，页面响应体限制为 2 MiB，抓取失败只会记录为警告。

“立即运行”会读取普通 Feed 当前响应中的全部条目，只保留北京时间今天和昨天，并同步完成每个来源最新 3 条的评分。其余内容只保存摘要，用户点击“分析此条”时才补抓全文并调用模型。模型调用共享 `400 RPM / 800,000 TPM` 的保守限流器：

每次运行都有独立进度记录。Web 会原地更新当前可见文章，并区分待分析、正在重试、等待模型额度和失败；模型返回的数字字符串、小数或越界 `score` 会自动规范化到 `0–10`，不会因这一字段反复消耗额度。

```yaml
profile:
  priority_terms: [Eino, AI Agent, Go, LLM]
  muted_feeds: ["低价值来源名称"]
  muted_tags: [招聘, 营销]
settings:
  max_candidates_per_run: 0
  max_items_per_feed: 0
  initial_items_per_feed: 3
  retention_days: 2
  analysis_rpm: 400
  analysis_tpm: 800000
  initial_token_budget: 700000
  analysis_cache_ttl: 168h
  full_text_min_chars: 600
  full_text_max_chars: 8000
```

偏好变化会改变缓存键，因此修改 `profile` 后文章会重新分析。

定时运行：

```powershell
go run ./cmd/rss-agent watch
```

## 多 Profile

根级配置是 `default` profile。可在 `profiles` 中定义独立的兴趣、订阅、设置、预算、推送与可选模型池；所有 profile 共享同一 SQLite 文件，但条目归属、已读状态和反馈记录彼此隔离。

```yaml
profiles:
  product:
    profile:
      interests: [AI 产品策略, 开发者工具, 产品增长]
      priority_terms: [AI, Agent, product]
    feeds:
      - name: Go Blog
        url: https://go.dev/blog/feed.atom
        tags: [product, developer-tools]
    push:
      console: true
```

profile 未设置 `models` 时会沿用根级模型池；设置后可使用独立的 `models.primary` 和 `models.fallback`。通过 `-profile` 选择运行上下文：

```powershell
go run ./cmd/rss-agent once -profile product
go run ./cmd/rss-agent review -profile product
go run ./cmd/rss-agent feedback list -profile product
go run ./cmd/rss-agent add -profile product -tag product "Product Hunt" "https://www.producthunt.com/feed"
```

## Eino Graph

每次运行由 Eino Graph 按以下节点顺序编排：

```text
fetch -> filter -> enrich -> analyze -> push
```

节点之间传递同一份运行状态，因此后续可在稳定节点边界上增加 tracing、回调、并行分支或人工确认，而不改变 RSS、缓存、模型和推送模块的职责。

## 模型与预算

每个模型可独立设置超时、温度、最大输出 token、价格与免费额度。`models.primary` 不可用时才尝试 `models.fallback`。

每一批模型输出都会校验固定字段、JSON 类型、0-10 分数范围，以及是否恰好覆盖输入的每个 `item_id`。校验或调用失败时，先用同一模型自动重试一次，仍失败才切换回退模型。

```yaml
models:
  primary:
    provider: ark
    api_key_env: ARK_API_KEY
    name: ${ARK_MODEL}
    input_price_cny_per_million: 0
    output_price_cny_per_million: 0
budget:
  llm_monthly_cny: 5
  hard_stop_cny: 19
```

在有未命中缓存的候选文章时，程序会先检查本月已记录的总成本和 LLM 成本；达到 `hard_stop_cny` 或 `llm_monthly_cny` 时停止新的模型调用。若要让成本估算生效，请按所用模型填写输入、输出价格。

## 订阅源管理

添加、查看订阅源：

```powershell
go run ./cmd/rss-agent add -profile default -tag go -tag engineering "Go Blog" "https://go.dev/blog/feed.atom"
go run ./cmd/rss-agent list -profile default
```

导入或导出 OPML。导入会按 URL 自动跳过已有订阅源：

```powershell
go run ./cmd/rss-agent import-opml -profile default subscriptions.opml
go run ./cmd/rss-agent export-opml -profile default feeds.opml
```

所有命令都支持 `-config` 指向另一份配置：

```powershell
go run ./cmd/rss-agent once -config .\work-config.yaml -dry-run
```

## 反馈与复盘

先查看最近由常规 `once` 保存过的条目，复制对应的 `item_id`：

```powershell
go run ./cmd/rss-agent review -profile default
```

记录反馈：

```powershell
go run ./cmd/rss-agent feedback like <item-id> -profile default
go run ./cmd/rss-agent feedback save <item-id> -profile default
go run ./cmd/rss-agent feedback later <item-id> -profile default
go run ./cmd/rss-agent feedback dislike <item-id> -profile default
go run ./cmd/rss-agent feedback block <item-id> -profile default
go run ./cmd/rss-agent feedback block-feed <item-id> -profile default
```

- `like`、`save`、`later` 会保存可复盘的状态。
- `dislike` 和 `block` 会阻止该条目再次进入后续筛选与模型分析。
- `block-feed` 会屏蔽同一 RSS 来源的未来文章。

查看或撤销反馈：

```powershell
go run ./cmd/rss-agent feedback list -profile default
go run ./cmd/rss-agent feedback list save -profile default
go run ./cmd/rss-agent feedback remove block-feed <item-id> -profile default
```

## 推送

默认结果会输出到控制台。通用 webhook 继续发送以下 JSON：

```json
{
  "text": "Markdown 摘要",
  "items": []
}
```

所有渠道都可同时启用；每条投递都会以真实渠道名写入 `push_records`。某个渠道失败不会阻止其他渠道投递；只要至少一个渠道成功，该条目就会标记为已投递，失败渠道仍会保留错误记录供后续检查。

```yaml
push:
  console: true
  webhook_url_env: RSS_AGENT_WEBHOOK_URL # 通用 JSON webhook
  feishu:
    webhook_url_env: FEISHU_WEBHOOK_URL
  dingtalk:
    webhook_url_env: DINGTALK_WEBHOOK_URL
  telegram:
    bot_token_env: TELEGRAM_BOT_TOKEN
    chat_id_env: TELEGRAM_CHAT_ID
  email:
    smtp_host: smtp.example.com
    smtp_port: 587
    username_env: RSS_AGENT_SMTP_USERNAME
    password_env: RSS_AGENT_SMTP_PASSWORD
    from: rss@example.com # 留空时使用 username
    to: [reader@example.com]
    subject: RSS Agent Digest
    start_tls: true
```

飞书使用机器人文本消息，钉钉使用 Markdown 消息，Telegram 使用 Bot API 的 Markdown 消息。邮件使用 SMTP；默认端口为 `587`，默认启用 STARTTLS。`webhook_url`、Telegram token 和 SMTP 密码都可以直接写入配置，但建议只使用对应的环境变量。

## Web 工作台

Web 工作台直接读取与 CLI 相同的 SQLite 数据库，提供订阅画像切换、Digest 筛选与搜索、文章详情、反馈记录和命令面板。默认仅监听本机回环地址：

工作台还提供主题筛选、来源健康状态和“投喂”入口。投喂链接会尝试读取页面标题与正文；X 等无法直接读取的页面可以同时填写标题或粘贴正文。`希望看同类`、`太浅`、`太理论`、`太营销`、`无法使用`会作为显式偏好信号保存在 SQLite。

```powershell
go run ./cmd/rss-agent serve
# 打开 http://127.0.0.1:8787
```

## AI 情报调度

`watch` 每小时增量抓取和分析，Digest 时间仍用于形成早报/晚报历史。当前版本不连接任何第三方推送渠道：

```yaml
settings:
  interval: 1h
digest:
  times: ["08:00", "20:00"]
  daily_limit: 12
  per_run_limit: 6
```

指定其他端口：

```powershell
go run ./cmd/rss-agent serve -addr 127.0.0.1:8790
```

页面上的“立即运行”会调用与 `rss-agent once` 相同的真实抓取和分析流程，可能产生模型费用；收藏、稍后阅读、手动投喂及历史 Digest 入选文章不会被两日清理删除。

## 命令速查

```text
rss-agent init [-config config.yaml]
rss-agent add [-config config.yaml] [-profile default] [-tag ai] [-tag go] <name> <url>
rss-agent list [-config config.yaml] [-profile default]
rss-agent import-opml [-config config.yaml] [-profile default] <subscriptions.opml>
rss-agent export-opml [-config config.yaml] [-profile default] <subscriptions.opml>
rss-agent review [-limit 20] [-config config.yaml] [-profile default]
rss-agent feedback <like|dislike|block|save|later|block-feed> <item-id> [-config config.yaml] [-profile default]
rss-agent feedback list [action] [-config config.yaml] [-profile default]
rss-agent feedback remove <action> <item-id> [-config config.yaml] [-profile default]
rss-agent once [-config config.yaml] [-profile default] [-dry-run] [-include-seen]
rss-agent watch [-config config.yaml] [-profile default]
rss-agent serve [-config config.yaml] [-addr 127.0.0.1:8787]
```

运行 `go run ./cmd/rss-agent help` 查看 CLI 帮助。

## 开发验证

```powershell
go test ./...
go build ./cmd/rss-agent
```

核心 LLM 调用位于 `internal/agent`，通过 `github.com/cloudwego/eino-ext/components/model/openai` 创建 Eino `ChatModel`；调度、缓存、预算和推送流程位于 `internal/app`。
