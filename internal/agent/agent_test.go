package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/jeffery/rss-agent/internal/config"
	"github.com/jeffery/rss-agent/internal/rss"
)

type scriptedModel struct {
	responses []string
	calls     int
}

func (m *scriptedModel) Generate(_ context.Context, _ []*schema.Message, _ ...einomodel.Option) (*schema.Message, error) {
	if m.calls >= len(m.responses) {
		return nil, fmt.Errorf("unexpected model call %d", m.calls+1)
	}
	content := m.responses[m.calls]
	m.calls++
	return &schema.Message{Content: content}, nil
}

func decisionJSON(itemID string) string {
	return fmt.Sprintf(`[{"item_id":"%s","score":8,"should_push":true,"title":"T","summary":"S","why":"W","key_points":["A"],"tags":["go"]}]`, itemID)
}

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

func TestParseDecisionsRejectsInvalidSchema(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "missing required field",
			content: `[{"item_id":"abc","score":8,"should_push":true,"title":"T","summary":"S","key_points":["A"],"tags":["go"]}]`,
		},
		{
			name:    "score out of range",
			content: `[{"item_id":"abc","score":11,"should_push":true,"title":"T","summary":"S","why":"W","key_points":["A"],"tags":["go"]}]`,
		},
		{
			name:    "unknown field",
			content: `[{"item_id":"abc","score":8,"should_push":true,"title":"T","summary":"S","why":"W","key_points":["A"],"tags":["go"],"confidence":0.9}]`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseDecisions(test.content); err == nil {
				t.Fatal("parseDecisions() error = nil, want schema validation error")
			}
		})
	}
}

func TestAnalyzeRetriesInvalidBatchOutput(t *testing.T) {
	model := &scriptedModel{responses: []string{
		decisionJSON("unexpected-item"),
		decisionJSON("item-1"),
	}}
	results, err := NewWithModel(model).Analyze(context.Background(), config.Profile{}, []rss.Item{{ID: "item-1", Title: "Title"}}, 1)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if model.calls != 2 {
		t.Fatalf("model calls = %d, want 2", model.calls)
	}
	if len(results) != 1 || results[0].Decision.ItemID != "item-1" {
		t.Fatalf("unexpected results: %+v", results)
	}
}

func TestAnalyzeFallsBackAfterRetry(t *testing.T) {
	primary := &scriptedModel{responses: []string{
		decisionJSON("unexpected-item"),
		decisionJSON("unexpected-item"),
	}}
	fallback := &scriptedModel{responses: []string{decisionJSON("item-1")}}
	a := &Agent{models: []modelEntry{
		{config: config.ResolvedModel{Label: "primary", Name: "primary", Provider: "test"}, model: primary},
		{config: config.ResolvedModel{Label: "fallback", Name: "fallback", Provider: "test"}, model: fallback},
	}}

	results, err := a.Analyze(context.Background(), config.Profile{}, []rss.Item{{ID: "item-1", Title: "Title"}}, 1)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if primary.calls != 2 || fallback.calls != 1 {
		t.Fatalf("model calls = primary %d, fallback %d; want 2 and 1", primary.calls, fallback.calls)
	}
	if len(results) != 1 || results[0].ModelLabel != "fallback" {
		t.Fatalf("unexpected results: %+v", results)
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
