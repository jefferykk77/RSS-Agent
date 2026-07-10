package app

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jeffery/rss-agent/internal/config"
)

func TestRunOnceCompletesGraphWithoutCandidates(t *testing.T) {
	cfg := config.Sample()
	cfg.Feeds = nil
	cfg.Database.Path = filepath.Join(t.TempDir(), "rss-agent.db")

	summary, err := RunOnce(context.Background(), cfg, RunOptions{DryRun: true})
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if summary.Fetched != 0 || summary.Candidate != 0 || summary.Analyzed != 0 || summary.Pushed != 0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}
