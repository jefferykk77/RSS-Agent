package agent

import (
	"strings"
	"testing"

	"github.com/jeffery/rss-agent/internal/config"
	"github.com/jeffery/rss-agent/internal/rss"
)

func TestParseDecisions(t *testing.T) {
	content := "```json\n[{\"item_id\":\"abc\",\"score\":8,\"should_push\":true,\"title\":\"T\",\"summary\":\"S\",\"why\":\"W\",\"key_points\":[\"A\"],\"tags\":[\"go\"]}]\n```"
	got, err := parseDecisions(content)
	if err != nil {
		t.Fatalf("parseDecisions() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].ItemID != "abc" || got[0].Score != 8 || !got[0].ShouldPush {
		t.Fatalf("unexpected decision: %+v", got[0])
	}
}

func TestBuildUserPayloadIncludesLocalTriageHints(t *testing.T) {
	payload := buildUserPayload(config.Profile{Language: "zh-CN", PriorityTerms: []string{"Eino"}}, []rss.Item{{
		ID:           "item-1",
		Title:        "Eino update",
		LocalScore:   5,
		LocalReasons: []string{"priority term: Eino (title)"},
	}})

	for _, want := range []string{"\"priority_terms\"", "\"local_score\": 5", "\"local_reasons\""} {
		if !strings.Contains(payload, want) {
			t.Fatalf("payload missing %q: %s", want, payload)
		}
	}
}
