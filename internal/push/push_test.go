package push

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeffery/rss-agent/internal/agent"
	"github.com/jeffery/rss-agent/internal/rss"
)

func TestPushPostsEachHTTPChannel(t *testing.T) {
	requests := make(map[string]map[string]any)
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode %s payload: %v", r.URL.Path, err)
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		mu.Lock()
		requests[r.URL.Path] = payload
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	pusher := &Pusher{
		WebhookURL:         server.URL + "/webhook",
		FeishuWebhookURL:   server.URL + "/feishu",
		DingTalkWebhookURL: server.URL + "/dingtalk",
		TelegramBotToken:   "test-token",
		TelegramChatID:     "test-chat",
		TelegramAPIBaseURL: server.URL,
	}
	deliveries, err := pusher.Push(context.Background(), []agent.Result{testResult()})
	if err != nil {
		t.Fatalf("Push() error = %v", err)
	}
	if len(deliveries) != 4 {
		t.Fatalf("delivery count = %d, want 4", len(deliveries))
	}
	for _, delivery := range deliveries {
		if delivery.Err != nil {
			t.Fatalf("%s delivery error = %v", delivery.Channel, delivery.Err)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if _, ok := requests["/webhook"]["items"]; !ok {
		t.Fatalf("generic webhook payload = %#v, want items", requests["/webhook"])
	}
	if got := requests["/feishu"]["msg_type"]; got != "text" {
		t.Fatalf("Feishu msg_type = %#v, want text", got)
	}
	if got := requests["/dingtalk"]["msgtype"]; got != "markdown" {
		t.Fatalf("DingTalk msgtype = %#v, want markdown", got)
	}
	if got := requests["/bottest-token/sendMessage"]["chat_id"]; got != "test-chat" {
		t.Fatalf("Telegram chat_id = %#v, want test-chat", got)
	}
}

func TestPushKeepsSuccessfulDeliveriesWhenAnotherChannelFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/feishu" {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	pusher := &Pusher{
		WebhookURL:       server.URL + "/webhook",
		FeishuWebhookURL: server.URL + "/feishu",
	}
	deliveries, err := pusher.Push(context.Background(), []agent.Result{testResult()})
	if err == nil {
		t.Fatal("Push() error = nil, want failed Feishu delivery")
	}
	if len(deliveries) != 2 {
		t.Fatalf("delivery count = %d, want 2", len(deliveries))
	}
	if deliveries[0].Channel != "webhook" || deliveries[0].Err != nil {
		t.Fatalf("webhook delivery = %+v, want success", deliveries[0])
	}
	if deliveries[1].Channel != "feishu" || deliveries[1].Err == nil {
		t.Fatalf("Feishu delivery = %+v, want failure", deliveries[1])
	}
}

func TestBuildEmailMessage(t *testing.T) {
	message := string(buildEmailMessage("rss@example.com", []string{"reader@example.com"}, "日报", "内容"))
	for _, expected := range []string{
		"From: rss@example.com\r\n",
		"To: reader@example.com\r\n",
		"Subject: =?UTF-8?",
		"Content-Type: text/plain; charset=UTF-8\r\n",
		"\r\n内容",
	} {
		if !strings.Contains(message, expected) {
			t.Fatalf("email message missing %q: %q", expected, message)
		}
	}
}

func testResult() agent.Result {
	item := rss.Item{
		ID:          "item-1",
		FeedName:    "Test Feed",
		Title:       "A useful update",
		Link:        "https://example.com/article",
		PublishedAt: time.Date(2026, time.July, 10, 12, 0, 0, 0, time.UTC),
	}
	return agent.Result{
		Item: item,
		Decision: agent.Decision{
			ItemID:     item.StableID(),
			Score:      9,
			ShouldPush: true,
			Summary:    "Summary",
			Why:        "Relevant",
			KeyPoints:  []string{"Point"},
			Tags:       []string{"go"},
		},
	}
}
