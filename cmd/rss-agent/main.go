package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/jeffery/rss-agent/internal/app"
	"github.com/jeffery/rss-agent/internal/config"
	"github.com/jeffery/rss-agent/internal/opml"
)

const defaultConfigPath = "config.yaml"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		usage()
		return nil
	}

	switch os.Args[1] {
	case "init":
		return initConfig(os.Args[2:])
	case "add":
		return addFeed(os.Args[2:])
	case "list":
		return listFeeds(os.Args[2:])
	case "import-opml":
		return importOPML(os.Args[2:])
	case "export-opml":
		return exportOPML(os.Args[2:])
	case "once":
		return runOnce(os.Args[2:])
	case "watch":
		return watch(os.Args[2:])
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		return fmt.Errorf("未知命令 %q\n\n运行 rss-agent help 查看用法", os.Args[1])
	}
}

func initConfig(args []string) error {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	configPath := fs.String("config", defaultConfigPath, "配置文件路径")
	force := fs.Bool("force", false, "覆盖已有配置")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if _, err := os.Stat(*configPath); err == nil && !*force {
		return fmt.Errorf("%s 已存在；如需覆盖请加 -force", *configPath)
	}
	if err := config.Save(*configPath, config.Sample()); err != nil {
		return err
	}
	fmt.Printf("已生成 %s。填好 profile、feeds、ARK_API_KEY 和 ARK_MODEL 后运行：go run ./cmd/rss-agent once\n", *configPath)
	return nil
}

func importOPML(args []string) error {
	fs := flag.NewFlagSet("import-opml", flag.ExitOnError)
	configPath := fs.String("config", defaultConfigPath, "配置文件路径")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("用法：rss-agent import-opml <subscriptions.opml> [-config config.yaml]")
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	feeds, err := opml.Import(fs.Arg(0))
	if err != nil {
		return err
	}
	existing := map[string]bool{}
	for _, feed := range cfg.Feeds {
		existing[feed.URL] = true
	}
	added := 0
	for _, feed := range feeds {
		if existing[feed.URL] {
			continue
		}
		cfg.Feeds = append(cfg.Feeds, feed)
		existing[feed.URL] = true
		added++
	}
	if err := config.Save(*configPath, cfg); err != nil {
		return err
	}
	fmt.Printf("已从 OPML 导入 %d 个新订阅源。\n", added)
	return nil
}

func exportOPML(args []string) error {
	fs := flag.NewFlagSet("export-opml", flag.ExitOnError)
	configPath := fs.String("config", defaultConfigPath, "配置文件路径")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("用法：rss-agent export-opml <subscriptions.opml> [-config config.yaml]")
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	if err := opml.Export(fs.Arg(0), cfg.Feeds); err != nil {
		return err
	}
	fmt.Printf("已导出 OPML：%s\n", fs.Arg(0))
	return nil
}

func addFeed(args []string) error {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	configPath := fs.String("config", defaultConfigPath, "配置文件路径")
	tags := multiFlag{}
	disabled := fs.Bool("disabled", false, "添加后先禁用")
	fs.Var(&tags, "tag", "订阅源标签，可重复")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 2 {
		return fmt.Errorf("用法：rss-agent add <name> <url> [-tag ai] [-tag go]")
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	cfg.Feeds = append(cfg.Feeds, config.Feed{
		Name:     fs.Arg(0),
		URL:      fs.Arg(1),
		Tags:     []string(tags),
		Disabled: *disabled,
	})
	if err := config.Save(*configPath, cfg); err != nil {
		return err
	}
	fmt.Printf("已添加订阅源：%s\n", fs.Arg(0))
	return nil
}

func listFeeds(args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	configPath := fs.String("config", defaultConfigPath, "配置文件路径")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	for i, feed := range cfg.Feeds {
		state := "enabled"
		if feed.Disabled {
			state = "disabled"
		}
		tagText := ""
		if len(feed.Tags) > 0 {
			tagText = " [" + strings.Join(feed.Tags, ", ") + "]"
		}
		fmt.Printf("%d. %s%s - %s (%s)\n", i+1, feed.Name, tagText, feed.URL, state)
	}
	return nil
}

func runOnce(args []string) error {
	fs := flag.NewFlagSet("once", flag.ExitOnError)
	configPath := fs.String("config", defaultConfigPath, "配置文件路径")
	dryRun := fs.Bool("dry-run", false, "只输出结果，不写入状态，不发 webhook")
	includeSeen := fs.Bool("include-seen", false, "包含已处理过的条目")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	ctx, stop := signalContext()
	defer stop()
	summary, err := app.RunOnce(ctx, cfg, app.RunOptions{
		DryRun:      *dryRun,
		IncludeSeen: *includeSeen,
	})
	for _, fetchErr := range summary.Errors {
		fmt.Fprintf(os.Stderr, "抓取警告：%v\n", fetchErr)
	}
	fmt.Printf("完成：抓取 %d 条，候选 %d 条，分析 %d 条，推送 %d 条。\n",
		summary.Fetched, summary.Candidate, summary.Analyzed, summary.Pushed)
	fmt.Printf("本地筛选：跳过 %d 条（重复 %d、已读 %d、过期 %d、静默 %d、排除 %d、未命中必须项 %d、候选限额 %d）。\n",
		summary.Triage.Skipped(),
		summary.Triage.Duplicate,
		summary.Triage.Seen,
		summary.Triage.Stale,
		summary.Triage.Muted,
		summary.Triage.Excluded,
		summary.Triage.MissingRequired,
		summary.Triage.Capped)
	return err
}

func watch(args []string) error {
	fs := flag.NewFlagSet("watch", flag.ExitOnError)
	configPath := fs.String("config", defaultConfigPath, "配置文件路径")
	includeSeen := fs.Bool("include-seen", false, "包含已处理过的条目")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	ctx, stop := signalContext()
	defer stop()
	return app.Watch(ctx, cfg, app.RunOptions{IncludeSeen: *includeSeen})
}

func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

type multiFlag []string

func (m *multiFlag) String() string {
	return strings.Join(*m, ",")
}

func (m *multiFlag) Set(value string) error {
	*m = append(*m, value)
	return nil
}

func usage() {
	fmt.Println(`RSS Agent

用法：
  rss-agent init [-config config.yaml]
  rss-agent add <name> <url> [-tag ai] [-tag go]
  rss-agent list [-config config.yaml]
  rss-agent import-opml <subscriptions.opml> [-config config.yaml]
  rss-agent export-opml <subscriptions.opml> [-config config.yaml]
  rss-agent once [-config config.yaml] [-dry-run] [-include-seen]
  rss-agent watch [-config config.yaml]

环境变量：
  ARK_API_KEY        火山方舟 API Key
  ARK_MODEL          火山方舟授权模型或接入点 ID
  DEEPSEEK_API_KEY   可选，DeepSeek fallback API Key
  DEEPSEEK_MODEL     可选，DeepSeek fallback 模型名
  RSS_AGENT_WEBHOOK_URL 可选，接收 JSON webhook 推送`)
}
