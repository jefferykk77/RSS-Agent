package agent

import "testing"

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
