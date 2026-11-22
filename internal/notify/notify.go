// Package notify sends a webhook message when an app goes down or recovers.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/abhixops/homeland/internal/config"
	"github.com/abhixops/homeland/internal/health"
)

const sendTimeout = 10 * time.Second

// Event describes a single up/down transition.
type Event struct {
	Time     time.Time `json:"time"`
	App      string    `json:"app"`
	URL      string    `json:"url"`
	Status   string    `json:"status"`
	Previous string    `json:"previous"`
	Error    string    `json:"error,omitempty"`
}

// Text returns a short human-readable summary of the event.
func (e Event) Text() string {
	if e.Status == string(health.StatusUp) {
		return fmt.Sprintf("✅ %s is back up", e.App)
	}
	if e.Error != "" {
		return fmt.Sprintf("🔴 %s is down (%s)", e.App, e.Error)
	}
	return fmt.Sprintf("🔴 %s is down", e.App)
}

// Notifier posts events to the webhook in the current config. It reads the
// config on every send, so changes apply on hot reload.
type Notifier struct {
	client    *http.Client
	getConfig func() config.NotifyCfg
}

// New creates a Notifier that reads its settings from getConfig.
func New(getConfig func() config.NotifyCfg) *Notifier {
	return &Notifier{client: &http.Client{Timeout: sendTimeout}, getConfig: getConfig}
}

// HandleTransition matches health.TransitionFunc and sends the event in the
// background so a slow webhook never delays health checks.
func (n *Notifier) HandleTransition(app config.App, previous, current health.Result) {
	cfg := n.getConfig()
	if cfg.WebhookURL == "" {
		return
	}
	event := Event{
		Time:     current.CheckedAt,
		App:      app.Name,
		URL:      app.URL,
		Status:   string(current.Status),
		Previous: string(previous.Status),
		Error:    current.Error,
	}
	go func() {
		if err := n.Send(context.Background(), cfg, event); err != nil {
			log.Printf("[notify] %v", err)
		}
	}()
}

// Send delivers one event using the given settings.
func (n *Notifier) Send(ctx context.Context, cfg config.NotifyCfg, event Event) error {
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()

	req, err := buildRequest(ctx, cfg, event)
	if err != nil {
		return err
	}
	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("send webhook: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func buildRequest(ctx context.Context, cfg config.NotifyCfg, event Event) (*http.Request, error) {
	if strings.EqualFold(cfg.Format, "ntfy") {
		// ntfy shows the request body as the message text.
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.WebhookURL, strings.NewReader(event.Text()))
		if err != nil {
			return nil, fmt.Errorf("build webhook request: %w", err)
		}
		req.Header.Set("Title", "Homeland: "+event.App)
		if event.Status == string(health.StatusDown) {
			req.Header.Set("Tags", "rotating_light")
			req.Header.Set("Priority", "high")
		} else {
			req.Header.Set("Tags", "white_check_mark")
		}
		return req, nil
	}

	// Generic JSON: the structured fields plus the summary under the keys
	// common chat webhooks read (text, content, message).
	body, err := json.Marshal(struct {
		Event
		Text    string `json:"text"`
		Content string `json:"content"`
		Message string `json:"message"`
		Title   string `json:"title"`
	}{Event: event, Text: event.Text(), Content: event.Text(), Message: event.Text(), Title: "Homeland: " + event.App})
	if err != nil {
		return nil, fmt.Errorf("encode webhook body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}
