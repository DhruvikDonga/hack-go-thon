package pgstore

import (
	"context"
	"testing"
	"time"
)

func TestWebhookStore_NilDBErrors(t *testing.T) {
	ctx := context.Background()

	if err := InitWebhookSchema(ctx, nil); err == nil {
		t.Errorf("expected error with nil db client for InitWebhookSchema")
	}

	sub := &WebhookModel{
		ID:          "wh_test_123",
		URL:         "https://example.com/webhook",
		Events:      []string{"notification"},
		Secret:      "secret123",
		Description: "Test subscription",
		Active:      true,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	if err := CreateWebhookSubscription(ctx, nil, sub); err == nil {
		t.Errorf("expected error with nil db client for CreateWebhookSubscription")
	}

	if _, err := ListWebhookSubscriptions(ctx, nil); err == nil {
		t.Errorf("expected error with nil db client for ListWebhookSubscriptions")
	}

	if _, err := GetWebhookSubscription(ctx, nil, "wh_test_123"); err == nil {
		t.Errorf("expected error with nil db client for GetWebhookSubscription")
	}

	if err := UpdateWebhookSubscription(ctx, nil, sub); err == nil {
		t.Errorf("expected error with nil db client for UpdateWebhookSubscription")
	}

	if err := DeleteWebhookSubscription(ctx, nil, "wh_test_123"); err == nil {
		t.Errorf("expected error with nil db client for DeleteWebhookSubscription")
	}

	logEntry := &WebhookDeliveryLogModel{
		ID:             "del_test_123",
		WebhookID:      "wh_test_123",
		URL:            "https://example.com/webhook",
		Event:          "notification",
		StatusCode:     200,
		DurationMs:     42,
		Success:        true,
		PayloadPreview: `{"event":"notification"}`,
		CreatedAt:      time.Now().UTC(),
	}

	if err := InsertWebhookDeliveryLog(ctx, nil, logEntry); err == nil {
		t.Errorf("expected error with nil db client for InsertWebhookDeliveryLog")
	}

	if _, err := ListWebhookDeliveryLogs(ctx, nil, 10); err == nil {
		t.Errorf("expected error with nil db client for ListWebhookDeliveryLogs")
	}
}
