package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"

	"github.com/tgruben-circuit/percy/db"
	"github.com/tgruben-circuit/percy/server/notifications"
)

// WebPushChannel sends notifications to all stored push subscriptions.
// It is registered as a built-in channel at startup (not user-configured).
type WebPushChannel struct {
	db           *db.DB
	logger       *slog.Logger
	vapidPrivKey string
	vapidPubKey  string
	client       *http.Client
}

// NewWebPushChannel creates a new web push channel.
func NewWebPushChannel(database *db.DB, vapidPrivKey, vapidPubKey string, logger *slog.Logger) *WebPushChannel {
	return &WebPushChannel{
		db:           database,
		logger:       logger,
		vapidPrivKey: vapidPrivKey,
		vapidPubKey:  vapidPubKey,
		client:       &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *WebPushChannel) Name() string { return "webpush" }

type pushPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Tag   string `json:"tag,omitempty"`
	URL   string `json:"url,omitempty"`
}

func (c *WebPushChannel) Send(ctx context.Context, event notifications.Event) error {
	payload := c.formatPayload(event)
	if payload == nil {
		return nil
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("webpush: marshal payload: %w", err)
	}

	subs, err := c.db.ListPushSubscriptions(ctx)
	if err != nil {
		return fmt.Errorf("webpush: list subscriptions: %w", err)
	}
	if len(subs) == 0 {
		return nil
	}

	var lastErr error
	for _, sub := range subs {
		resp, err := webpush.SendNotificationWithContext(ctx, data, &webpush.Subscription{
			Endpoint: sub.Endpoint,
			Keys: webpush.Keys{
				Auth:   sub.Auth,
				P256dh: sub.P256DH,
			},
		}, &webpush.Options{
			HTTPClient:      c.client,
			Subscriber:      "mailto:shelley@localhost",
			VAPIDPublicKey:  c.vapidPubKey,
			VAPIDPrivateKey: c.vapidPrivKey,
			TTL:             86400,
			Urgency:         webpush.UrgencyNormal,
		})
		if err != nil {
			c.logger.Warn("webpush: send failed", "endpoint", truncateEndpoint(sub.Endpoint), "error", err)
			lastErr = err
			continue
		}
		resp.Body.Close()

		if resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusNotFound {
			// Subscription is expired/invalid — remove it
			c.logger.Info("webpush: removing expired subscription", "endpoint", truncateEndpoint(sub.Endpoint))
			if delErr := c.db.DeletePushSubscription(context.Background(), sub.ID); delErr != nil {
				c.logger.Warn("webpush: failed to delete expired subscription", "error", delErr)
			}
		} else if resp.StatusCode != http.StatusCreated {
			err = fmt.Errorf("push service returned %d", resp.StatusCode)
			c.logger.Warn("webpush: unexpected status", "status", resp.StatusCode, "endpoint", truncateEndpoint(sub.Endpoint))
			lastErr = err
		}
	}
	return lastErr
}

func (c *WebPushChannel) formatPayload(event notifications.Event) *pushPayload {
	convoURL := "/conversation/" + event.ConversationID
	switch event.Type {
	case notifications.EventAgentDone:
		p, ok := event.Payload.(notifications.AgentDonePayload)
		if !ok {
			return &pushPayload{Title: "Percy", Body: "Agent finished", URL: convoURL}
		}
		title := "Percy: agent finished"
		if p.ConversationTitle != "" {
			title = "Percy: " + p.ConversationTitle
		}
		body := p.FinalResponse
		if len(body) > 200 {
			body = body[:197] + "..."
		}
		if body == "" {
			body = "Agent finished"
		}
		return &pushPayload{
			Title: title,
			Body:  body,
			Tag:   "percy-done-" + event.ConversationID,
			URL:   convoURL,
		}

	case notifications.EventAgentError:
		p, ok := event.Payload.(notifications.AgentErrorPayload)
		if !ok {
			return &pushPayload{Title: "Percy", Body: "Agent error", URL: convoURL}
		}
		body := p.ErrorMessage
		if body == "" {
			body = "Agent encountered an error"
		}
		return &pushPayload{
			Title: "Percy: agent error",
			Body:  body,
			Tag:   "percy-error-" + event.ConversationID,
			URL:   convoURL,
		}

	default:
		return nil
	}
}

func truncateEndpoint(endpoint string) string {
	if len(endpoint) > 60 {
		return endpoint[:57] + "..."
	}
	return endpoint
}
