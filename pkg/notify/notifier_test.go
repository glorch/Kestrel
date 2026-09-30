package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestFormatPayloads(t *testing.T) {
	msg := &Message{
		Title:       "Pipeline Completed",
		Content:     "All 12 jobs succeeded in 45s",
		Type:        NotifyPipelineSuccess,
		Pipeline:    "kestrel-ci",
		RunID:       "run-101",
		Status:      "PASSED",
		ActionURL:   "https://ci.example.com/runs/run-101",
	}

	// 1. Feishu
	_, feishuBytes, err := FormatPayload(ChannelConfig{Type: "feishu", Secret: "test-secret"}, msg)
	if err != nil {
		t.Fatalf("feishu format error: %v", err)
	}
	var feishuMap map[string]interface{}
	if err := json.Unmarshal(feishuBytes, &feishuMap); err != nil {
		t.Fatalf("feishu json unmarshal error: %v", err)
	}
	if feishuMap["msg_type"] != "interactive" {
		t.Errorf("expected feishu msg_type 'interactive', got: %v", feishuMap["msg_type"])
	}
	if feishuMap["sign"] == "" {
		t.Error("expected non-empty sign for signed feishu config")
	}

	// 2. WeCom
	_, wecomBytes, err := FormatPayload(ChannelConfig{Type: "wecom"}, msg)
	if err != nil {
		t.Fatalf("wecom format error: %v", err)
	}
	var wecomMap map[string]interface{}
	if err := json.Unmarshal(wecomBytes, &wecomMap); err != nil {
		t.Fatalf("wecom json unmarshal error: %v", err)
	}
	if wecomMap["msgtype"] != "markdown" {
		t.Errorf("expected wecom msgtype 'markdown', got: %v", wecomMap["msgtype"])
	}

	// 3. DingTalk
	dingURL, dingBytes, err := FormatPayload(ChannelConfig{Type: "dingtalk", Webhook: "https://oapi.dingtalk.com/robot/send?access_token=123", Secret: "secret123"}, msg)
	if err != nil {
		t.Fatalf("dingtalk format error: %v", err)
	}
	if !strings.Contains(dingURL, "sign=") || !strings.Contains(dingURL, "timestamp=") {
		t.Errorf("expected dingtalk url to contain sign and timestamp, got: %s", dingURL)
	}
	var dingMap map[string]interface{}
	if err := json.Unmarshal(dingBytes, &dingMap); err != nil {
		t.Fatalf("dingtalk json unmarshal error: %v", err)
	}
	if dingMap["msgtype"] != "markdown" {
		t.Errorf("expected dingtalk msgtype 'markdown', got: %v", dingMap["msgtype"])
	}

	// 4. Slack
	_, slackBytes, err := FormatPayload(ChannelConfig{Type: "slack"}, msg)
	if err != nil {
		t.Fatalf("slack format error: %v", err)
	}
	var slackMap map[string]interface{}
	if err := json.Unmarshal(slackBytes, &slackMap); err != nil {
		t.Fatalf("slack json unmarshal error: %v", err)
	}
	if slackMap["blocks"] == nil {
		t.Error("expected slack blocks to be present")
	}

	// 5. Generic
	_, genericBytes, err := FormatPayload(ChannelConfig{Type: "generic"}, msg)
	if err != nil {
		t.Fatalf("generic format error: %v", err)
	}
	var genericMsg Message
	if err := json.Unmarshal(genericBytes, &genericMsg); err != nil {
		t.Fatalf("generic json unmarshal error: %v", err)
	}
	if genericMsg.RunID != "run-101" {
		t.Errorf("expected run-101, got: %s", genericMsg.RunID)
	}
}

func TestDispatcherSend(t *testing.T) {
	var receivedCount int32

	tsSuccess := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&receivedCount, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errcode":0}`))
	}))
	defer tsSuccess.Close()

	tsFailure := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer tsFailure.Close()

	channels := []ChannelConfig{
		{
			Name:    "prod-feishu",
			Type:    "feishu",
			Webhook: tsSuccess.URL,
			Enabled: true,
		},
		{
			Name:    "prod-wecom",
			Type:    "wecom",
			Webhook: tsSuccess.URL,
			Enabled: true,
		},
		{
			Name:    "disabled-channel",
			Type:    "slack",
			Webhook: tsSuccess.URL,
			Enabled: false, // Should not receive
		},
		{
			Name:    "failing-channel",
			Type:    "generic",
			Webhook: tsFailure.URL,
			Enabled: true, // Returns 500
		},
	}

	dispatcher := NewDispatcher(channels)

	msg := &Message{
		Title:    "Deployment Approved",
		Content:  "Operator alice approved production rollout",
		Type:     NotifyApprovalRequest,
		Pipeline: "deploy-pipeline",
		RunID:    "run-500",
		Status:   "APPROVED",
	}

	results := dispatcher.Send(context.Background(), msg)

	if len(results) != 3 { // 3 enabled channels
		t.Fatalf("expected 3 results for active channels, got: %d", len(results))
	}

	if atomic.LoadInt32(&receivedCount) != 2 {
		t.Errorf("expected 2 successful calls to mock server, got: %d", receivedCount)
	}

	for _, res := range results {
		if res.ChannelName == "failing-channel" {
			if res.Success {
				t.Errorf("expected failing-channel to fail, but succeeded")
			}
			if res.StatusCode != 500 {
				t.Errorf("expected status code 500, got: %d", res.StatusCode)
			}
		} else {
			if !res.Success {
				t.Errorf("expected channel '%s' to succeed, got error: %s", res.ChannelName, res.Error)
			}
		}
	}
}
