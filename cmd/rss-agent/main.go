package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jeffery/rss-agent/internal/app"
	"github.com/jeffery/rss-agent/internal/config"
	"github.com/jeffery/rss-agent/internal/opml"
	"github.com/jeffery/rss-agent/internal/store"
	"github.com/jeffery/rss-agent/internal/web"
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
	case "feedback":
		return feedback(os.Args[2:])
	case "review":
		return review(os.Args[2:])
	case "once":
		return runOnce(os.Args[2:])
	case "watch":
		return watch(os.Args[2:])
	case "serve":
		return serve(os.Args[2:])
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

func loadConfigProfile(configPath string, profileName string) (*config.Config, *config.Config, string, error) {
	base, err := config.Load(configPath)
	if err != nil {
		return nil, nil, "", err
	}
	profileID := strings.TrimSpace(profileName)
	if profileID == "" {
		profileID = config.DefaultProfileName
	}
	resolved, err := base.ResolveProfile(profileID)
	if err != nil {
		return nil, nil, "", err
	}
	return base, resolved, profileID, nil
}

func saveProfileFeeds(cfg *config.Config, profileID string, feeds []config.Feed) error {
	feeds = append([]config.Feed(nil), feeds...)
	if profileID == config.DefaultProfileName {
		cfg.Feeds = feeds
		return nil
	}
	profile, ok := cfg.Profiles[profileID]
	if !ok {
		return fmt.Errorf("未找到 profile %q", profileID)
	}
	profile.Feeds = feeds
	cfg.Profiles[profileID] = profile
	return nil
}

func importOPML(args []string) error {
	fs := flag.NewFlagSet("import-opml", flag.ExitOnError)
	configPath := fs.String("config", defaultConfigPath, "配置文件路径")
	profileName := fs.String("profile", config.DefaultProfileName, "profile 名称")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("用法：rss-agent import-opml <subscriptions.opml> [-config config.yaml]")
	}
	base, cfg, profileID, err := loadConfigProfile(*configPath, *profileName)
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
	if err := saveProfileFeeds(base, profileID, cfg.Feeds); err != nil {
		return err
	}
	if err := config.Save(*configPath, base); err != nil {
		return err
	}
	fmt.Printf("已从 OPML 导入 %d 个新订阅源。\n", added)
	return nil
}

func exportOPML(args []string) error {
	fs := flag.NewFlagSet("export-opml", flag.ExitOnError)
	configPath := fs.String("config", defaultConfigPath, "配置文件路径")
	profileName := fs.String("profile", config.DefaultProfileName, "profile 名称")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("用法：rss-agent export-opml <subscriptions.opml> [-config config.yaml]")
	}
	_, cfg, _, err := loadConfigProfile(*configPath, *profileName)
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
	profileName := fs.String("profile", config.DefaultProfileName, "profile 名称")
	tags := multiFlag{}
	disabled := fs.Bool("disabled", false, "添加后先禁用")
	fs.Var(&tags, "tag", "订阅源标签，可重复")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 2 {
		return fmt.Errorf("用法：rss-agent add <name> <url> [-tag ai] [-tag go]")
	}
	base, cfg, profileID, err := loadConfigProfile(*configPath, *profileName)
	if err != nil {
		return err
	}
	cfg.Feeds = append(cfg.Feeds, config.Feed{
		Name:     fs.Arg(0),
		URL:      fs.Arg(1),
		Tags:     []string(tags),
		Disabled: *disabled,
	})
	if err := saveProfileFeeds(base, profileID, cfg.Feeds); err != nil {
		return err
	}
	if err := config.Save(*configPath, base); err != nil {
		return err
	}
	fmt.Printf("已添加订阅源：%s\n", fs.Arg(0))
	return nil
}

func listFeeds(args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	configPath := fs.String("config", defaultConfigPath, "配置文件路径")
	profileName := fs.String("profile", config.DefaultProfileName, "profile 名称")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_, cfg, _, err := loadConfigProfile(*configPath, *profileName)
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

func feedback(args []string) error {
	configPath, profileName, positional, err := feedbackArgs(args)
	if err != nil {
		return err
	}
	if len(positional) == 0 {
		return fmt.Errorf("用法：rss-agent feedback <like|dislike|block|save|later|block-feed> <item-id> [-config config.yaml]")
	}
	_, cfg, profileID, err := loadConfigProfile(configPath, profileName)
	if err != nil {
		return err
	}
	db, err := store.Open(cfg.DatabasePath())
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, stop := signalContext()
	defer stop()

	switch positional[0] {
	case "list":
		var action store.FeedbackAction
		if len(positional) > 2 {
			return fmt.Errorf("用法：rss-agent feedback list [action] [-config config.yaml]")
		}
		if len(positional) == 2 {
			action, err = store.ParseFeedbackAction(positional[1])
			if err != nil {
				return err
			}
		}
		entries, err := db.ListFeedbackForProfile(ctx, profileID, action)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			fmt.Println("暂无反馈记录。")
			return nil
		}
		for _, entry := range entries {
			title := entry.Title
			if title == "" {
				title = entry.ItemID
			}
			fmt.Printf("%s  %-10s  %s — %s\n",
				entry.CreatedAt.Local().Format("2006-01-02 15:04"),
				entry.Action,
				title,
				entry.FeedName,
			)
		}
		return nil
	case "remove":
		if len(positional) != 3 {
			return fmt.Errorf("用法：rss-agent feedback remove <action> <item-id> [-config config.yaml]")
		}
		action, err := store.ParseFeedbackAction(positional[1])
		if err != nil {
			return err
		}
		removed, err := db.RemoveFeedbackForProfile(ctx, profileID, positional[2], action)
		if err != nil {
			return err
		}
		if !removed {
			return fmt.Errorf("条目 %q 没有 %s 反馈", positional[2], action)
		}
		fmt.Printf("已移除 %s：%s\n", action, positional[2])
		return nil
	default:
		if len(positional) != 2 {
			return fmt.Errorf("用法：rss-agent feedback <like|dislike|block|save|later|block-feed> <item-id> [-config config.yaml]")
		}
		action, err := store.ParseFeedbackAction(positional[0])
		if err != nil {
			return err
		}
		entry, err := db.RecordFeedbackForProfile(ctx, profileID, positional[1], action)
		if err != nil {
			return err
		}
		fmt.Printf("已记录 %s：%s\n", entry.Action, entry.Title)
		return nil
	}
}

func feedbackArgs(args []string) (string, string, []string, error) {
	configPath := defaultConfigPath
	profileName := config.DefaultProfileName
	positional := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "-config":
			if i+1 >= len(args) {
				return "", "", nil, errors.New("-config 需要配置文件路径")
			}
			configPath = args[i+1]
			i++
		case strings.HasPrefix(args[i], "-config="):
			configPath = strings.TrimPrefix(args[i], "-config=")
		case args[i] == "-profile":
			if i+1 >= len(args) {
				return "", "", nil, errors.New("-profile 需要 profile 名称")
			}
			profileName = args[i+1]
			i++
		case strings.HasPrefix(args[i], "-profile="):
			profileName = strings.TrimPrefix(args[i], "-profile=")
		default:
			positional = append(positional, args[i])
		}
	}
	return configPath, profileName, positional, nil
}

func review(args []string) error {
	fs := flag.NewFlagSet("review", flag.ExitOnError)
	configPath := fs.String("config", defaultConfigPath, "配置文件路径")
	profileName := fs.String("profile", config.DefaultProfileName, "profile 名称")
	limit := fs.Int("limit", 20, "显示最近条目数")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *limit <= 0 {
		return errors.New("-limit 必须大于 0")
	}
	_, cfg, profileID, err := loadConfigProfile(*configPath, *profileName)
	if err != nil {
		return err
	}
	db, err := store.Open(cfg.DatabasePath())
	if err != nil {
		return err
	}
	defer db.Close()
	items, err := db.RecentItemsForProfile(context.Background(), profileID, *limit)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		fmt.Println("暂无已保存条目。先运行一次不带 -dry-run 的 once。")
		return nil
	}
	for _, item := range items {
		published := "未知时间"
		if !item.PublishedAt.IsZero() {
			published = item.PublishedAt.Local().Format("2006-01-02 15:04")
		}
		fmt.Printf("%s  %s\n  %s · %s\n  %s\n", item.ID, item.Title, item.FeedName, published, item.Link)
	}
	return nil
}

func runOnce(args []string) error {
	fs := flag.NewFlagSet("once", flag.ExitOnError)
	configPath := fs.String("config", defaultConfigPath, "配置文件路径")
	profileName := fs.String("profile", config.DefaultProfileName, "profile 名称")
	dryRun := fs.Bool("dry-run", false, "只输出结果，不写入状态，不发 webhook")
	includeSeen := fs.Bool("include-seen", false, "包含已处理过的条目")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_, cfg, profileID, err := loadConfigProfile(*configPath, *profileName)
	if err != nil {
		return err
	}
	ctx, stop := signalContext()
	defer stop()
	summary, err := app.RunOnce(ctx, cfg, app.RunOptions{
		DryRun:      *dryRun,
		IncludeSeen: *includeSeen,
		ProfileID:   profileID,
	})
	for _, fetchErr := range summary.Errors {
		fmt.Fprintf(os.Stderr, "抓取警告：%v\n", fetchErr)
	}
	fmt.Printf("完成：抓取 %d 条，候选 %d 条，分析 %d 条，推送 %d 条。\n",
		summary.Fetched, summary.Candidate, summary.Analyzed, summary.Pushed)
	fmt.Printf("本地筛选：跳过 %d 条（重复 %d、已读 %d、过期 %d、静默 %d、反馈屏蔽 %d、排除 %d、未命中必须项 %d、候选限额 %d）。\n",
		summary.Triage.Skipped(),
		summary.Triage.Duplicate,
		summary.Triage.Seen,
		summary.Triage.Stale,
		summary.Triage.Muted,
		summary.Triage.FeedbackBlocked,
		summary.Triage.Excluded,
		summary.Triage.MissingRequired,
		summary.Triage.Capped)
	return err
}

func watch(args []string) error {
	fs := flag.NewFlagSet("watch", flag.ExitOnError)
	configPath := fs.String("config", defaultConfigPath, "配置文件路径")
	profileName := fs.String("profile", config.DefaultProfileName, "profile 名称")
	includeSeen := fs.Bool("include-seen", false, "包含已处理过的条目")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_, cfg, profileID, err := loadConfigProfile(*configPath, *profileName)
	if err != nil {
		return err
	}
	ctx, stop := signalContext()
	defer stop()
	return app.Watch(ctx, cfg, app.RunOptions{IncludeSeen: *includeSeen, ProfileID: profileID})
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	configPath := fs.String("config", defaultConfigPath, "配置文件路径")
	addr := fs.String("addr", "127.0.0.1:8787", "监听地址")
	if err := fs.Parse(args); err != nil {
		return err
	}
	base, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	db, err := store.Open(base.DatabasePath())
	if err != nil {
		return err
	}
	defer db.Close()

	server := &http.Server{
		Addr:              *addr,
		Handler:           web.New(base, db).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	ctx, stop := signalContext()
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	fmt.Printf("RSS Agent Web UI: http://%s\n", *addr)
	err = server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
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
  rss-agent add [-config config.yaml] [-profile default] [-tag ai] [-tag go] <name> <url>
  rss-agent list [-config config.yaml] [-profile default]
  rss-agent import-opml [-config config.yaml] [-profile default] <subscriptions.opml>
  rss-agent export-opml [-config config.yaml] [-profile default] <subscriptions.opml>
  rss-agent feedback <like|dislike|block|save|later|block-feed> <item-id> [-config config.yaml] [-profile default]
  rss-agent feedback list [action] [-config config.yaml] [-profile default]
  rss-agent feedback remove <action> <item-id> [-config config.yaml] [-profile default]
  rss-agent review [-limit 20] [-config config.yaml] [-profile default]
  rss-agent once [-config config.yaml] [-profile default] [-dry-run] [-include-seen]
  rss-agent watch [-config config.yaml] [-profile default]
  rss-agent serve [-config config.yaml] [-addr 127.0.0.1:8787]

环境变量：
  ARK_API_KEY        火山方舟 API Key
  ARK_MODEL          火山方舟授权模型或接入点 ID
  DEEPSEEK_API_KEY   可选，DeepSeek fallback API Key
  DEEPSEEK_MODEL     可选，DeepSeek fallback 模型名
  RSS_AGENT_WEBHOOK_URL 可选，接收 JSON webhook 推送`)
}
