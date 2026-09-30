package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// NotificationType identifies the semantic nature of an alert.
type NotificationType string

const (
	NotifyPipelineSuccess NotificationType = "PIPELINE_SUCCESS"
	NotifyPipelineFailure NotificationType = "PIPELINE_FAILURE"
	NotifyApprovalRequest NotificationType = "APPROVAL_REQUEST"
	NotifyCanaryRollback  NotificationType = "CANARY_ROLLBACK"
	NotifyCustom          NotificationType = "CUSTOM"
)

// Message is the channel-agnostic notification event model.
type Message struct {
	Title       string                 `json:"title"`
	Content     string                 `json:"content"`
	Type        NotificationType       `json:"type"`
	Pipeline    string                 `json:"pipeline,omitempty"`
	RunID       string                 `json:"run_id,omitempty"`
	Status      string                 `json:"status,omitempty"`
	Environment string                 `json:"environment,omitempty"`
	GateID      string                 `json:"gate_id,omitempty"`
	ActionURL   string                 `json:"action_url,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// ChannelConfig represents an endpoint receiver for notifications.
type ChannelConfig struct {
	Name    string `json:"name,omitempty" yaml:"name,omitempty"`
	Type    string `json:"type" yaml:"type"` // "feishu", "wecom", "dingtalk", "slack", "generic"
	Webhook string `json:"webhook" yaml:"webhook"`
	Secret  string `json:"secret,omitempty" yaml:"secret,omitempty"` // For signature verification
	Enabled bool   `json:"enabled" yaml:"enabled"`
}

// DispatchResult captures delivery details per channel.
type DispatchResult struct {
	ChannelName string `json:"channel_name"`
	Type        string `json:"type"`
	Success     bool   `json:"success"`
	StatusCode  int    `json:"status_code,omitempty"`
	Error       string `json:"error,omitempty"`
}

// Dispatcher coordinates multi-channel notification dispatch.
type Dispatcher struct {
	mu       sync.RWMutex
	channels []ChannelConfig
	client   *http.Client
}

// NewDispatcher creates a notification hub.
func NewDispatcher(channels []ChannelConfig) *Dispatcher {
	return &Dispatcher{
		channels: channels,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// AddChannel registers a new alert target.
func (d *Dispatcher) AddChannel(cfg ChannelConfig) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.channels = append(d.channels, cfg)
}

// Send broadcasts the message to all active notification channels concurrently.
func (d *Dispatcher) Send(ctx context.Context, msg *Message) []DispatchResult {
	d.mu.RLock()
	active := make([]ChannelConfig, 0, len(d.channels))
	for _, ch := range d.channels {
		if ch.Enabled {
			active = append(active, ch)
		}
	}
	d.mu.RUnlock()

	results := make([]DispatchResult, len(active))
	if len(active) == 0 {
		return results
	}

	var wg sync.WaitGroup
	for i, ch := range active {
		wg.Add(1)
		go func(idx int, target ChannelConfig) {
			defer wg.Done()
			results[idx] = d.sendToChannel(ctx, target, msg)
		}(i, ch)
	}
	wg.Wait()

	return results
}

func (d *Dispatcher) sendToChannel(ctx context.Context, ch ChannelConfig, msg *Message) DispatchResult {
	res := DispatchResult{
		ChannelName: ch.Name,
		Type:        ch.Type,
	}

	reqURL, payload, err := FormatPayload(ch, msg)
	if err != nil {
		res.Success = false
		res.Error = fmt.Sprintf("format error: %v", err)
		return res
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(payload))
	if err != nil {
		res.Success = false
		res.Error = fmt.Sprintf("request init error: %v", err)
		return res
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := d.client.Do(req)
	if err != nil {
		res.Success = false
		res.Error = err.Error()
		return res
	}
	defer resp.Body.Close()

	res.StatusCode = resp.StatusCode
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		res.Success = true
	} else {
		res.Success = false
		res.Error = fmt.Sprintf("webhook responded with HTTP status %d", resp.StatusCode)
	}

	return res
}

// FormatPayload formats a platform-compliant JSON body and URL for the specified target.
func FormatPayload(ch ChannelConfig, msg *Message) (string, []byte, error) {
	reqURL := ch.Webhook

	switch strings.ToLower(ch.Type) {
	case "feishu", "lark":
		// Feishu interactive card or rich text
		body := map[string]interface{}{
			"msg_type": "interactive",
			"card": map[string]interface{}{
				"header": map[string]interface{}{
					"title": map[string]string{
						"tag":     "plain_text",
						"content": msg.Title,
					},
					"template": getFeishuTemplateColor(msg.Type),
				},
				"elements": []map[string]interface{}{
					{
						"tag": "div",
						"text": map[string]string{
							"tag":     "lark_md",
							"content": fmt.Sprintf("**Pipeline:** %s\n**Run ID:** %s\n**Status:** %s\n\n%s", msg.Pipeline, msg.RunID, msg.Status, msg.Content),
						},
					},
				},
			},
		}

		if ch.Secret != "" {
			timestamp := time.Now().Unix()
			body["timestamp"] = strconv.FormatInt(timestamp, 10)
			body["sign"] = generateFeishuSign(ch.Secret, timestamp)
		}

		data, err := json.Marshal(body)
		return reqURL, data, err

	case "wecom", "workwechat":
		// WeChat Work Markdown payload
		mdContent := fmt.Sprintf("### %s\n> **Pipeline:** <font color=\"comment\">%s</font>\n> **Run ID:** %s\n> **Status:** %s\n\n%s",
			msg.Title, msg.Pipeline, msg.RunID, msg.Status, msg.Content)
		if msg.ActionURL != "" {
			mdContent += fmt.Sprintf("\n\n[点击查看流水线详情](%s)", msg.ActionURL)
		}

		body := map[string]interface{}{
			"msgtype": "markdown",
			"markdown": map[string]string{
				"content": mdContent,
			},
		}
		data, err := json.Marshal(body)
		return reqURL, data, err

	case "dingtalk":
		// DingTalk ActionCard / Markdown
		if ch.Secret != "" {
			timestamp := time.Now().UnixMilli()
			sign := generateDingTalkSign(ch.Secret, timestamp)
			sep := "?"
			if strings.Contains(reqURL, "?") {
				sep = "&"
			}
			reqURL = fmt.Sprintf("%s%stimestamp=%d&sign=%s", reqURL, sep, timestamp, sign)
		}

		mdText := fmt.Sprintf("### %s\n\n- **Pipeline:** %s\n- **Run ID:** %s\n- **Status:** %s\n\n%s",
			msg.Title, msg.Pipeline, msg.RunID, msg.Status, msg.Content)

		body := map[string]interface{}{
			"msgtype": "markdown",
			"markdown": map[string]string{
				"title": msg.Title,
				"text":  mdText,
			},
		}
		data, err := json.Marshal(body)
		return reqURL, data, err

	case "slack":
		// Slack Block Kit payload
		body := map[string]interface{}{
			"text": fmt.Sprintf("*%s*: %s", msg.Title, msg.Content),
			"blocks": []map[string]interface{}{
				{
					"type": "header",
					"text": map[string]string{
						"type": "plain_text",
						"text": msg.Title,
					},
				},
				{
					"type": "section",
					"fields": []map[string]string{
						{"type": "mrkdwn", "text": fmt.Sprintf("*Pipeline:*\n%s", msg.Pipeline)},
						{"type": "mrkdwn", "text": fmt.Sprintf("*Status:*\n%s", msg.Status)},
					},
				},
				{
					"type": "section",
					"text": map[string]string{
						"type": "mrkdwn",
						"text": msg.Content,
					},
				},
			},
		}
		data, err := json.Marshal(body)
		return reqURL, data, err

	default:
		// Generic webhook
		data, err := json.Marshal(msg)
		return reqURL, data, err
	}
}

func getFeishuTemplateColor(t NotificationType) string {
	switch t {
	case NotifyPipelineSuccess:
		return "green"
	case NotifyPipelineFailure, NotifyCanaryRollback:
		return "red"
	case NotifyApprovalRequest:
		return "orange"
	default:
		return "blue"
	}
}

func generateFeishuSign(secret string, timestamp int64) string {
	stringToSign := fmt.Sprintf("%d\n%s", timestamp, secret)
	h := hmac.New(sha256.New, []byte(stringToSign))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func generateDingTalkSign(secret string, timestamp int64) string {
	stringToSign := fmt.Sprintf("%d\n%s", timestamp, secret)
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}
