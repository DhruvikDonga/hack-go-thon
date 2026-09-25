package handler

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestWebhookHandler_CRUDAndDispatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewWebhookHandler(nil, 2*time.Second)

	r := gin.New()
	r.POST("/webhooks", h.Register)
	r.GET("/webhooks", h.List)
	r.GET("/webhooks/:id", h.Get)
	r.PUT("/webhooks/:id", h.Update)
	r.DELETE("/webhooks/:id", h.Delete)
	r.POST("/webhooks/send", h.Send)
	r.POST("/webhooks/test", h.Test)
	r.GET("/webhooks/logs", h.GetLogs)

	// 1. Create a dummy receiver server to receive webhooks
	var receivedHeaders http.Header
	var receivedBody []byte
	receivedCh := make(chan bool, 1)

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		receivedHeaders = req.Header.Clone()
		receivedBody, _ = io.ReadAll(req.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"received": true}`))
		receivedCh <- true
	}))
	defer testServer.Close()

	// 2. Register Webhook
	secret := "test-secret-key-123"
	regBody := map[string]any{
		"url":         testServer.URL,
		"events":      []string{"notification", "system_alert"},
		"secret":      secret,
		"description": "Mobile Test Endpoint",
	}
	regBytes, _ := json.Marshal(regBody)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/webhooks", bytes.NewReader(regBytes))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
	}

	var regResp struct {
		Success bool                `json:"success"`
		Data    WebhookSubscription `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &regResp); err != nil {
		t.Fatalf("failed to parse registration response: %v", err)
	}

	webhookID := regResp.Data.ID
	if webhookID == "" || regResp.Data.URL != testServer.URL {
		t.Fatalf("unexpected webhook data: %+v", regResp.Data)
	}

	// 3. List Webhooks
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/webhooks", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on list, got %d", w.Code)
	}

	// 4. Get Webhook by ID
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/webhooks/"+webhookID, nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on get, got %d", w.Code)
	}

	// 5. Update Webhook
	updateBody := map[string]any{
		"description": "Updated Mobile Test Endpoint",
	}
	upBytes, _ := json.Marshal(updateBody)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/webhooks/"+webhookID, bytes.NewReader(upBytes))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on update, got %d: %s", w.Code, w.Body.String())
	}

	// 6. Test Ping on Demand
	testReqBody := map[string]any{
		"webhook_id": webhookID,
		"event":      "ping.test",
		"title":      "Ping Test",
		"message":    "Ping latency check",
	}
	testBytes, _ := json.Marshal(testReqBody)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/webhooks/test", bytes.NewReader(testBytes))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on test ping, got %d: %s", w.Code, w.Body.String())
	}

	var testResp struct {
		Success bool              `json:"success"`
		Data    WebhookTestResult `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &testResp); err != nil {
		t.Fatalf("failed to parse test response: %v", err)
	}
	if !testResp.Data.Success || testResp.Data.StatusCode != http.StatusOK {
		t.Fatalf("expected test ping success, got %+v", testResp.Data)
	}

	// Drain the ping message from receivedCh before testing async send
	select {
	case <-receivedCh:
	case <-time.After(1 * time.Second):
	}

	// 7. Dispatch Notification via Send
	sendBody := map[string]any{
		"event":    "notification",
		"title":    "New Message",
		"message":  "Hello mobile user!",
		"target":   "mobile_user_42",
		"priority": "high",
		"data": map[string]any{
			"badge": 1,
		},
	}
	sendBytes, _ := json.Marshal(sendBody)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/webhooks/send", bytes.NewReader(sendBytes))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on send, got %d: %s", w.Code, w.Body.String())
	}

	// Wait for asynchronous dispatch
	select {
	case <-receivedCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for webhook HTTP delivery")
	}

	// Verify HMAC signature
	sigHeader := receivedHeaders.Get("X-Webhook-Signature")
	if !strings.HasPrefix(sigHeader, "sha256=") {
		t.Fatalf("expected X-Webhook-Signature header, got %s", sigHeader)
	}
	receivedSig := strings.TrimPrefix(sigHeader, "sha256=")

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(receivedBody)
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	if receivedSig != expectedSig {
		t.Errorf("HMAC mismatch! expected %s, got %s", expectedSig, receivedSig)
	}

	if receivedHeaders.Get("X-Webhook-Event") != "notification" {
		t.Errorf("unexpected X-Webhook-Event: %s", receivedHeaders.Get("X-Webhook-Event"))
	}

	// 8. Check Delivery Logs
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/webhooks/logs", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on logs, got %d", w.Code)
	}

	var logsResp struct {
		Success bool                 `json:"success"`
		Data    []WebhookDeliveryLog `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &logsResp); err != nil {
		t.Fatalf("failed to parse logs: %v", err)
	}
	if len(logsResp.Data) == 0 {
		t.Fatalf("expected delivery logs, got 0")
	}

	// 9. Delete Webhook
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("DELETE", "/webhooks/"+webhookID, nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on delete, got %d", w.Code)
	}
}
