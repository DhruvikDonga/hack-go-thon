package handler

import (
	dbclient "hack-go-thon/internal/db_client"
	llmclient "hack-go-thon/internal/llm_client"
	pgstore "hack-go-thon/internal/store/pg_store"
	"hack-go-thon/pkg/apperrors"
	"hack-go-thon/pkg/response"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ExampleHandler demonstrates route handling, validation, database integration, and LLM completions.
type ExampleHandler struct {
	db  *dbclient.PostgresDatabase
	llm *llmclient.LLMClient
}

// NewExampleHandler creates a new ExampleHandler with optional db and llm clients.
func NewExampleHandler(db *dbclient.PostgresDatabase, llm *llmclient.LLMClient) *ExampleHandler {
	return &ExampleHandler{
		db:  db,
		llm: llm,
	}
}

// CreateItemRequest defines the request payload for creating an item.
type CreateItemRequest struct {
	Name        string `json:"name" binding:"required,min=2,max=100"`
	Description string `json:"description" binding:"max=255"`
}

// CreateItem godoc
// @Summary Create a new item
// @Description Demonstrates payload validation, PostgreSQL persistence (if configured), and response formatting
// @Tags items
// @Accept json
// @Produce json
// @Param request body CreateItemRequest true "Item Request Payload"
// @Success 201 {object} response.Response
// @Failure 400 {object} response.Response
// @Router /api/v1/items [post]
func (h *ExampleHandler) CreateItem(c *gin.Context) {
	var req CreateItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, apperrors.NewBadRequest("Invalid request payload", err.Error()))
		return
	}

	itemID := "item_" + uuid.New().String()[:8]

	// Persistent store mode if database client is active
	if h.db != nil {
		item := &pgstore.ItemModel{
			ID:          itemID,
			Title:       req.Name,
			Description: req.Description,
			Status:      "active",
		}

		if err := pgstore.CreateItem(c.Request.Context(), h.db, item); err != nil {
			response.Error(c, apperrors.NewInternal("Failed to persist item in database", err))
			return
		}

		response.Created(c, item)
		return
	}

	// In-memory fallback if database is not configured
	item := gin.H{
		"id":          itemID,
		"name":        req.Name,
		"description": req.Description,
	}

	response.Created(c, item)
}

// GetItem godoc
// @Summary Get item by ID
// @Description Demonstrates path param parsing, PostgreSQL lookup, and 404 handling
// @Tags items
// @Produce json
// @Param id path string true "Item ID"
// @Success 200 {object} response.Response
// @Failure 404 {object} response.Response
// @Router /api/v1/items/{id} [get]
func (h *ExampleHandler) GetItem(c *gin.Context) {
	id := c.Param("id")

	if h.db != nil {
		item, err := pgstore.GetItemByID(c.Request.Context(), h.db, id)
		if err != nil {
			response.Error(c, apperrors.NewInternal("Database query failed", err))
			return
		}
		if item == nil {
			response.Error(c, apperrors.NewNotFound("Item with specified ID not found"))
			return
		}

		response.OK(c, item)
		return
	}

	if id == "notfound" {
		response.Error(c, apperrors.NewNotFound("Item with specified ID not found"))
		return
	}

	response.OK(c, gin.H{
		"id":          id,
		"name":        "Sample Item",
		"description": "Demonstration item data",
	})
}

// ListItems godoc
// @Summary List items
// @Description Demonstrates list query with pagination
// @Tags items
// @Produce json
// @Success 200 {object} response.Response
// @Router /api/v1/items [get]
func (h *ExampleHandler) ListItems(c *gin.Context) {
	if h.db != nil {
		items, err := pgstore.ListItems(c.Request.Context(), h.db, 20, 0)
		if err != nil {
			response.Error(c, apperrors.NewInternal("Failed to query items", err))
			return
		}
		response.OK(c, items)
		return
	}

	// Fallback mock items
	response.OK(c, []gin.H{
		{"id": "item_1", "name": "Item 1", "description": "Mock item 1"},
		{"id": "item_2", "name": "Item 2", "description": "Mock item 2"},
	})
}

// AskAIRequest defines payload for LLM query.
type AskAIRequest struct {
	Prompt string `json:"prompt" binding:"required"`
	Model  string `json:"model"`
}

// AskAI godoc
// @Summary Query LLM Assistant
// @Description Demonstrates integration with OpenAI LLM client
// @Tags llm
// @Accept json
// @Produce json
// @Param request body AskAIRequest true "LLM Prompt"
// @Success 200 {object} response.Response
// @Failure 400 {object} response.Response
// @Router /api/v1/llm/ask [post]
func (h *ExampleHandler) AskAI(c *gin.Context) {
	if h.llm == nil {
		response.Error(c, apperrors.NewBadRequest("LLM client is not configured (missing OPENAI_API_KEY)"))
		return
	}

	var req AskAIRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, apperrors.NewBadRequest("Invalid request payload", err.Error()))
		return
	}

	ans, err := h.llm.GenerateChatCompletion(c.Request.Context(), req.Model, req.Prompt)
	if err != nil {
		response.Error(c, apperrors.NewInternal("LLM generation failed", err))
		return
	}

	response.OK(c, gin.H{
		"prompt":   req.Prompt,
		"response": ans,
	})
}
