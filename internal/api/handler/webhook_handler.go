package handler

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	dbclient "hack-go-thon/internal/db_client"
	pgstore "hack-go-thon/internal/store/pg_store"
	"hack-go-thon/internal/ws"
	"hack-go-thon/pkg/apperrors"
	"hack-go-thon/pkg/log"
	"hack-go-thon/pkg/response"

	"github.com/DhruvikDonga/simplysocket"
	"github.com/gin-gonic/gin"
)

// WebhookSubscription represents a registered webhook endpoint.
type WebhookSubscription struct {
	ID          string    `json:"id"`
	URL         string    `json:"url" binding:"required"`
	Events      []string  `json:"events"`           // e.g. ["*"] or ["notification", "alert"]
	Secret      string    `json:"secret,omitempty"` // Shared secret for HMAC-SHA256 signature
	Description string    `json:"description,omitempty"`
	Active      bool      `json:"active"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// NotificationPayload defines the event notification sent to webhooks and mobile clients.
type NotificationPayload struct {
	Event     string         `json:"event"` // e.g. "notification", "system_announcement", "chat_message"
	Title     string         `json:"title" binding:"required"`
	Message   string         `json:"message" binding:"required"`
	Data      map[string]any `json:"data,omitempty"`     // Custom JSON state for mobile client
	Target    string         `json:"target,omitempty"`   // Device token, user ID, or audience tag
	Priority  string         `json:"priority,omitempty"` // "normal" or "high"
	Timestamp string         `json:"timestamp,omitempty"`
}

// WebhookDeliveryLog records the status of an HTTP delivery attempt.
type WebhookDeliveryLog struct {
	ID             string    `json:"id"`
	WebhookID      string    `json:"webhook_id"`
	URL            string    `json:"url"`
	Event          string    `json:"event"`
	StatusCode     int       `json:"status_code"`
	DurationMs     int64     `json:"duration_ms"`
	Success        bool      `json:"success"`
	Error          string    `json:"error,omitempty"`
	Timestamp      time.Time `json:"timestamp"`
	PayloadPreview string    `json:"payload_preview,omitempty"`
}

// WebhookTestRequest is the input for testing webhook delivery.
type WebhookTestRequest struct {
	WebhookID string         `json:"webhook_id,omitempty"`
	URL       string         `json:"url,omitempty"`
	Secret    string         `json:"secret,omitempty"`
	Event     string         `json:"event,omitempty"`
	Title     string         `json:"title,omitempty"`
	Message   string         `json:"message,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
}

// WebhookTestResult returns immediate delivery metrics from an on-demand ping test.
type WebhookTestResult struct {
	Success      bool   `json:"success"`
	StatusCode   int    `json:"status_code"`
	DurationMs   int64  `json:"duration_ms"`
	ResponseBody string `json:"response_body,omitempty"`
	Error        string `json:"error,omitempty"`
}

// WebhookHandler coordinates webhook subscriptions, notification dispatching, and delivery logs.
type WebhookHandler struct {
	db            *dbclient.PostgresDatabase
	httpClient    *http.Client
	wsManager     *ws.Manager
	mu            sync.RWMutex
	subscriptions map[string]*WebhookSubscription
	logs          []WebhookDeliveryLog
	maxLogs       int
}

// NewWebhookHandler initializes the webhook notification handler.
// If db is provided and connected, webhook subscriptions and delivery logs are persisted in PostgreSQL.
// Otherwise, it operates purely in-memory.
func NewWebhookHandler(db *dbclient.PostgresDatabase, wsManager *ws.Manager, timeout time.Duration) *WebhookHandler {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	h := &WebhookHandler{
		db:            db,
		httpClient:    &http.Client{Timeout: timeout},
		wsManager:     wsManager,
		subscriptions: make(map[string]*WebhookSubscription),
		logs:          make([]WebhookDeliveryLog, 0, 100),
		maxLogs:       100,
	}

	if db != nil && db.Client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		dbSubs, err := pgstore.ListWebhookSubscriptions(ctx, db)
		if err == nil {
			for _, s := range dbSubs {
				h.subscriptions[s.ID] = &WebhookSubscription{
					ID:          s.ID,
					URL:         s.URL,
					Events:      s.Events,
					Secret:      s.Secret,
					Description: s.Description,
					Active:      s.Active,
					CreatedAt:   s.CreatedAt,
					UpdatedAt:   s.UpdatedAt,
				}
			}
			log.Info("Loaded webhook subscriptions from database", "count", len(dbSubs))
		} else {
			log.Warn("Failed to load initial webhook subscriptions from database", "error", err.Error())
		}
	}

	return h
}

// Register creates a new webhook subscription.
// POST /api/v1/webhooks
func (h *WebhookHandler) Register(c *gin.Context) {
	var input struct {
		URL         string   `json:"url" binding:"required"`
		Events      []string `json:"events"`
		Secret      string   `json:"secret"`
		Description string   `json:"description"`
		Active      *bool    `json:"active"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		response.Error(c, apperrors.NewBadRequest("Invalid webhook registration payload: "+err.Error()))
		return
	}

	trimmedURL := strings.TrimSpace(input.URL)
	if !strings.HasPrefix(trimmedURL, "http://") && !strings.HasPrefix(trimmedURL, "https://") {
		response.Error(c, apperrors.NewBadRequest("Webhook URL must start with http:// or https://"))
		return
	}

	events := input.Events
	if len(events) == 0 {
		events = []string{"*"}
	}

	isActive := true
	if input.Active != nil {
		isActive = *input.Active
	}

	id := generateRandomID("wh")
	now := time.Now().UTC()

	sub := &WebhookSubscription{
		ID:          id,
		URL:         trimmedURL,
		Events:      events,
		Secret:      strings.TrimSpace(input.Secret),
		Description: strings.TrimSpace(input.Description),
		Active:      isActive,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if h.db != nil && h.db.Client != nil {
		dbModel := &pgstore.WebhookModel{
			ID:          sub.ID,
			URL:         sub.URL,
			Events:      sub.Events,
			Secret:      sub.Secret,
			Description: sub.Description,
			Active:      sub.Active,
			CreatedAt:   sub.CreatedAt,
			UpdatedAt:   sub.UpdatedAt,
		}
		if err := pgstore.CreateWebhookSubscription(c.Request.Context(), h.db, dbModel); err != nil {
			response.Error(c, apperrors.NewInternal("Failed to persist webhook: "+err.Error()))
			return
		}
	}

	h.mu.Lock()
	h.subscriptions[id] = sub
	h.mu.Unlock()

	log.Info("Registered new webhook subscription", "id", id, "url", trimmedURL, "events", events)
	response.Created(c, sub)
}

// List returns all registered webhooks.
// GET /api/v1/webhooks
func (h *WebhookHandler) List(c *gin.Context) {
	if h.db != nil && h.db.Client != nil {
		dbSubs, err := pgstore.ListWebhookSubscriptions(c.Request.Context(), h.db)
		if err == nil {
			list := make([]*WebhookSubscription, 0, len(dbSubs))
			for _, s := range dbSubs {
				list = append(list, &WebhookSubscription{
					ID:          s.ID,
					URL:         s.URL,
					Events:      s.Events,
					Secret:      s.Secret,
					Description: s.Description,
					Active:      s.Active,
					CreatedAt:   s.CreatedAt,
					UpdatedAt:   s.UpdatedAt,
				})
			}
			response.OK(c, list)
			return
		}
		log.Warn("Failed to list webhooks from database, falling back to cache", "error", err.Error())
	}

	h.mu.RLock()
	list := make([]*WebhookSubscription, 0, len(h.subscriptions))
	for _, sub := range h.subscriptions {
		list = append(list, sub)
	}
	h.mu.RUnlock()

	response.OK(c, list)
}

// Get returns details for a specific webhook.
// GET /api/v1/webhooks/:id
func (h *WebhookHandler) Get(c *gin.Context) {
	id := c.Param("id")

	if h.db != nil && h.db.Client != nil {
		s, err := pgstore.GetWebhookSubscription(c.Request.Context(), h.db, id)
		if err == nil && s != nil {
			response.OK(c, &WebhookSubscription{
				ID:          s.ID,
				URL:         s.URL,
				Events:      s.Events,
				Secret:      s.Secret,
				Description: s.Description,
				Active:      s.Active,
				CreatedAt:   s.CreatedAt,
				UpdatedAt:   s.UpdatedAt,
			})
			return
		}
		if err != nil {
			log.Warn("Failed to get webhook from database, checking cache", "id", id, "error", err.Error())
		}
	}

	h.mu.RLock()
	sub, exists := h.subscriptions[id]
	h.mu.RUnlock()

	if !exists {
		response.Error(c, apperrors.NewNotFound("Webhook subscription not found"))
		return
	}

	response.OK(c, sub)
}

// Update modifies an existing webhook subscription.
// PUT /api/v1/webhooks/:id
func (h *WebhookHandler) Update(c *gin.Context) {
	id := c.Param("id")

	var input struct {
		URL         *string   `json:"url"`
		Events      *[]string `json:"events"`
		Secret      *string   `json:"secret"`
		Description *string   `json:"description"`
		Active      *bool     `json:"active"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		response.Error(c, apperrors.NewBadRequest("Invalid update payload: "+err.Error()))
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	sub, exists := h.subscriptions[id]
	if !exists && h.db != nil && h.db.Client != nil {
		dbSub, err := pgstore.GetWebhookSubscription(c.Request.Context(), h.db, id)
		if err == nil && dbSub != nil {
			sub = &WebhookSubscription{
				ID:          dbSub.ID,
				URL:         dbSub.URL,
				Events:      dbSub.Events,
				Secret:      dbSub.Secret,
				Description: dbSub.Description,
				Active:      dbSub.Active,
				CreatedAt:   dbSub.CreatedAt,
				UpdatedAt:   dbSub.UpdatedAt,
			}
			exists = true
		}
	}

	if !exists {
		response.Error(c, apperrors.NewNotFound("Webhook subscription not found"))
		return
	}

	if input.URL != nil {
		trimmed := strings.TrimSpace(*input.URL)
		if !strings.HasPrefix(trimmed, "http://") && !strings.HasPrefix(trimmed, "https://") {
			response.Error(c, apperrors.NewBadRequest("Webhook URL must start with http:// or https://"))
			return
		}
		sub.URL = trimmed
	}
	if input.Events != nil {
		sub.Events = *input.Events
	}
	if input.Secret != nil {
		sub.Secret = strings.TrimSpace(*input.Secret)
	}
	if input.Description != nil {
		sub.Description = strings.TrimSpace(*input.Description)
	}
	if input.Active != nil {
		sub.Active = *input.Active
	}
	sub.UpdatedAt = time.Now().UTC()

	if h.db != nil && h.db.Client != nil {
		dbModel := &pgstore.WebhookModel{
			ID:          sub.ID,
			URL:         sub.URL,
			Events:      sub.Events,
			Secret:      sub.Secret,
			Description: sub.Description,
			Active:      sub.Active,
			CreatedAt:   sub.CreatedAt,
			UpdatedAt:   sub.UpdatedAt,
		}
		if err := pgstore.UpdateWebhookSubscription(c.Request.Context(), h.db, dbModel); err != nil {
			response.Error(c, apperrors.NewInternal("Failed to update webhook in database: "+err.Error()))
			return
		}
	}

	h.subscriptions[id] = sub
	response.OK(c, sub)
}

// Delete removes a webhook subscription.
// DELETE /api/v1/webhooks/:id
func (h *WebhookHandler) Delete(c *gin.Context) {
	id := c.Param("id")

	h.mu.Lock()
	_, exists := h.subscriptions[id]
	if exists {
		delete(h.subscriptions, id)
	}
	h.mu.Unlock()

	if h.db != nil && h.db.Client != nil {
		err := pgstore.DeleteWebhookSubscription(c.Request.Context(), h.db, id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) && !exists {
				response.Error(c, apperrors.NewNotFound("Webhook subscription not found"))
				return
			}
			if !errors.Is(err, sql.ErrNoRows) {
				response.Error(c, apperrors.NewInternal("Failed to delete webhook from database: "+err.Error()))
				return
			}
		} else {
			exists = true
		}
	}

	if !exists {
		response.Error(c, apperrors.NewNotFound("Webhook subscription not found"))
		return
	}

	response.OK(c, gin.H{"deleted": true, "id": id})
}

// Send dispatches a notification payload to all active webhooks that match the event.
// Also broadcasts to simplysocket WebSocket mesh (MeshGlobalRoom).
// POST /api/v1/webhooks/send
func (h *WebhookHandler) Send(c *gin.Context) {
	var payload NotificationPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		response.Error(c, apperrors.NewBadRequest("Invalid notification payload: "+err.Error()))
		return
	}

	if payload.Event == "" {
		payload.Event = "notification"
	}
	if payload.Timestamp == "" {
		payload.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}
	if payload.Priority == "" {
		payload.Priority = "normal"
	}

	// 1. Find matching subscriptions
	h.mu.RLock()
	var targets []*WebhookSubscription
	for _, sub := range h.subscriptions {
		if !sub.Active {
			continue
		}
		if matchesEvent(sub.Events, payload.Event) {
			targets = append(targets, sub)
		}
	}
	h.mu.RUnlock()

	// 2. Dispatch to webhooks asynchronously
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		response.Error(c, apperrors.NewInternal("Failed to encode payload: "+err.Error()))
		return
	}

	for _, sub := range targets {
		go h.dispatchSingle(sub, payload.Event, payloadBytes)
	}

	// 3. Mirror broadcast to WebSocket mesh if active
	if h.wsManager != nil {
		h.wsManager.Broadcast(simplysocket.MeshGlobalRoom, "webhook-notification", map[string]any{
			"event":     payload.Event,
			"title":     payload.Title,
			"message":   payload.Message,
			"data":      payload.Data,
			"target":    payload.Target,
			"priority":  payload.Priority,
			"timestamp": payload.Timestamp,
		})
	}

	response.OK(c, gin.H{
		"dispatched":    true,
		"targets_count": len(targets),
		"event":         payload.Event,
		"timestamp":     payload.Timestamp,
	})
}

// Test sends a live test payload to a target webhook or raw URL and reports immediate response metrics.
// POST /api/v1/webhooks/test
func (h *WebhookHandler) Test(c *gin.Context) {
	var req WebhookTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, apperrors.NewBadRequest("Invalid test request: "+err.Error()))
		return
	}

	targetURL := strings.TrimSpace(req.URL)
	secret := strings.TrimSpace(req.Secret)

	// If webhook_id specified, load URL and Secret from subscription
	if req.WebhookID != "" {
		h.mu.RLock()
		sub, exists := h.subscriptions[req.WebhookID]
		h.mu.RUnlock()
		if !exists {
			response.Error(c, apperrors.NewNotFound("Webhook subscription not found: "+req.WebhookID))
			return
		}
		targetURL = sub.URL
		if secret == "" {
			secret = sub.Secret
		}
	}

	if targetURL == "" {
		response.Error(c, apperrors.NewBadRequest("Target URL or valid Webhook ID is required"))
		return
	}

	event := req.Event
	if event == "" {
		event = "test.ping"
	}
	title := req.Title
	if title == "" {
		title = "Test Webhook Ping"
	}
	message := req.Message
	if message == "" {
		message = "Connectivity verification from Hack-Go-Thon backend."
	}

	testPayload := map[string]any{
		"event":     event,
		"title":     title,
		"message":   message,
		"data":      req.Data,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}

	payloadBytes, _ := json.Marshal(testPayload)
	deliveryID := generateRandomID("del")

	httpReq, err := http.NewRequestWithContext(c.Request.Context(), "POST", targetURL, bytes.NewReader(payloadBytes))
	if err != nil {
		response.OK(c, WebhookTestResult{
			Success: false,
			Error:   "Failed to create HTTP request: " + err.Error(),
		})
		return
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Webhook-Event", event)
	httpReq.Header.Set("X-Webhook-Delivery", deliveryID)
	httpReq.Header.Set("X-Webhook-Timestamp", time.Now().UTC().Format(time.RFC3339))
	httpReq.Header.Set("User-Agent", "Hack-Go-Thon-Webhook/1.0")

	if secret != "" {
		sig := computeHMAC(payloadBytes, secret)
		httpReq.Header.Set("X-Webhook-Signature", "sha256="+sig)
	}

	start := time.Now()
	resp, reqErr := h.httpClient.Do(httpReq)
	duration := time.Since(start).Milliseconds()

	if reqErr != nil {
		result := WebhookTestResult{
			Success:    false,
			DurationMs: duration,
			Error:      reqErr.Error(),
		}
		h.recordLog(WebhookDeliveryLog{
			ID:             deliveryID,
			WebhookID:      req.WebhookID,
			URL:            targetURL,
			Event:          event,
			DurationMs:     duration,
			Success:        false,
			Error:          reqErr.Error(),
			Timestamp:      time.Now().UTC(),
			PayloadPreview: string(payloadBytes),
		})
		response.OK(c, result)
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	success := resp.StatusCode >= 200 && resp.StatusCode < 300

	result := WebhookTestResult{
		Success:      success,
		StatusCode:   resp.StatusCode,
		DurationMs:   duration,
		ResponseBody: string(respBody),
	}
	if !success {
		result.Error = fmt.Sprintf("HTTP status %d", resp.StatusCode)
	}

	h.recordLog(WebhookDeliveryLog{
		ID:             deliveryID,
		WebhookID:      req.WebhookID,
		URL:            targetURL,
		Event:          event,
		StatusCode:     resp.StatusCode,
		DurationMs:     duration,
		Success:        success,
		Error:          result.Error,
		Timestamp:      time.Now().UTC(),
		PayloadPreview: string(payloadBytes),
	})

	response.OK(c, result)
}

// GetLogs returns recent webhook delivery logs.
// GET /api/v1/webhooks/logs
func (h *WebhookHandler) GetLogs(c *gin.Context) {
	if h.db != nil && h.db.Client != nil {
		dbLogs, err := pgstore.ListWebhookDeliveryLogs(c.Request.Context(), h.db, h.maxLogs)
		if err == nil {
			logs := make([]WebhookDeliveryLog, 0, len(dbLogs))
			for _, l := range dbLogs {
				logs = append(logs, WebhookDeliveryLog{
					ID:             l.ID,
					WebhookID:      l.WebhookID,
					URL:            l.URL,
					Event:          l.Event,
					StatusCode:     l.StatusCode,
					DurationMs:     l.DurationMs,
					Success:        l.Success,
					Error:          l.Error,
					Timestamp:      l.CreatedAt,
					PayloadPreview: l.PayloadPreview,
				})
			}
			response.OK(c, logs)
			return
		}
		log.Warn("Failed to query webhook delivery logs from database, falling back to memory", "error", err.Error())
	}

	h.mu.RLock()
	logsCopy := make([]WebhookDeliveryLog, len(h.logs))
	copy(logsCopy, h.logs)
	h.mu.RUnlock()

	response.OK(c, logsCopy)
}

// dispatchSingle sends an HTTP POST request to a single subscriber and records the result.
func (h *WebhookHandler) dispatchSingle(sub *WebhookSubscription, event string, payloadBytes []byte) {
	deliveryID := generateRandomID("del")
	nowStr := time.Now().UTC().Format(time.RFC3339)

	req, err := http.NewRequest("POST", sub.URL, bytes.NewReader(payloadBytes))
	if err != nil {
		h.recordLog(WebhookDeliveryLog{
			ID:             deliveryID,
			WebhookID:      sub.ID,
			URL:            sub.URL,
			Event:          event,
			Success:        false,
			Error:          err.Error(),
			Timestamp:      time.Now().UTC(),
			PayloadPreview: string(payloadBytes),
		})
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Event", event)
	req.Header.Set("X-Webhook-Delivery", deliveryID)
	req.Header.Set("X-Webhook-Timestamp", nowStr)
	req.Header.Set("User-Agent", "Hack-Go-Thon-Webhook/1.0")

	if sub.Secret != "" {
		sig := computeHMAC(payloadBytes, sub.Secret)
		req.Header.Set("X-Webhook-Signature", "sha256="+sig)
	}

	start := time.Now()
	resp, reqErr := h.httpClient.Do(req)
	duration := time.Since(start).Milliseconds()

	logEntry := WebhookDeliveryLog{
		ID:             deliveryID,
		WebhookID:      sub.ID,
		URL:            sub.URL,
		Event:          event,
		DurationMs:     duration,
		Timestamp:      time.Now().UTC(),
		PayloadPreview: string(payloadBytes),
	}

	if reqErr != nil {
		logEntry.Success = false
		logEntry.Error = reqErr.Error()
		log.Warn("Webhook delivery failed", "webhook_id", sub.ID, "url", sub.URL, "error", reqErr.Error())
	} else {
		_ = resp.Body.Close()
		logEntry.StatusCode = resp.StatusCode
		logEntry.Success = resp.StatusCode >= 200 && resp.StatusCode < 300
		if !logEntry.Success {
			logEntry.Error = fmt.Sprintf("HTTP status %d", resp.StatusCode)
		}
	}

	h.recordLog(logEntry)
}

// recordLog adds a delivery log entry to the ring buffer and writes to PostgreSQL if connected.
func (h *WebhookHandler) recordLog(entry WebhookDeliveryLog) {
	h.mu.Lock()
	// Prepend for newest-first ordering
	h.logs = append([]WebhookDeliveryLog{entry}, h.logs...)
	if len(h.logs) > h.maxLogs {
		h.logs = h.logs[:h.maxLogs]
	}
	h.mu.Unlock()

	if h.db != nil && h.db.Client != nil {
		go func(e WebhookDeliveryLog) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = pgstore.InsertWebhookDeliveryLog(ctx, h.db, &pgstore.WebhookDeliveryLogModel{
				ID:             e.ID,
				WebhookID:      e.WebhookID,
				URL:            e.URL,
				Event:          e.Event,
				StatusCode:     e.StatusCode,
				DurationMs:     e.DurationMs,
				Success:        e.Success,
				Error:          e.Error,
				PayloadPreview: e.PayloadPreview,
				CreatedAt:      e.Timestamp,
			})
		}(entry)
	}
}

// matchesEvent checks if an event list matches the target event.
func matchesEvent(events []string, target string) bool {
	for _, e := range events {
		if e == "*" || strings.EqualFold(e, target) {
			return true
		}
	}
	return false
}

// computeHMAC calculates HMAC-SHA256 hex string.
func computeHMAC(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// generateRandomID generates a random prefixed string ID.
func generateRandomID(prefix string) string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s_%d_%x", prefix, time.Now().Unix(), b)
}
