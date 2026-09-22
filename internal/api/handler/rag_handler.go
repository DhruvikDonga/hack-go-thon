package handler

import (
	"fmt"
	"strings"
	"sync"
	"time"

	dbclient "hack-go-thon/internal/db_client"
	llmclient "hack-go-thon/internal/llm_client"
	pgstore "hack-go-thon/internal/store/pg_store"
	"hack-go-thon/pkg/apperrors"
	"hack-go-thon/pkg/response"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// RAGHandler handles document indexing, vector similarity search, and RAG Q&A workflows.
type RAGHandler struct {
	db  *dbclient.PostgresDatabase
	llm *llmclient.LLMClient

	// In-memory fallback if PostgreSQL is not connected
	fallbackMu   sync.RWMutex
	fallbackDocs []*pgstore.DocumentModel
}

// NewRAGHandler initializes a new RAGHandler instance.
func NewRAGHandler(db *dbclient.PostgresDatabase, llm *llmclient.LLMClient) *RAGHandler {
	return &RAGHandler{
		db:           db,
		llm:          llm,
		fallbackDocs: make([]*pgstore.DocumentModel, 0),
	}
}

// CreateDocumentRequest defines the payload for indexing a new document.
type CreateDocumentRequest struct {
	Title    string         `json:"title" binding:"required,min=2,max=255"`
	Content  string         `json:"content" binding:"required,min=5"`
	Metadata map[string]any `json:"metadata"`
}

// CreateDocument indexes a new document, generates its vector embedding, and stores it in pgvector.
func (h *RAGHandler) CreateDocument(c *gin.Context) {
	var req CreateDocumentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, apperrors.NewBadRequest("Invalid request payload", err.Error()))
		return
	}

	ctx := c.Request.Context()
	docID := "doc_" + uuid.New().String()[:8]

	// 1. Generate embedding vector
	var embedding []float32
	var err error
	if h.llm != nil {
		embedding, err = h.llm.GenerateEmbedding(ctx, req.Title+"\n"+req.Content)
	} else {
		embedding = llmclient.GenerateMockEmbedding(req.Title + "\n" + req.Content)
	}
	if err != nil {
		response.Error(c, apperrors.NewInternal("Failed to generate embedding", err))
		return
	}

	doc := &pgstore.DocumentModel{
		ID:        docID,
		Title:     req.Title,
		Content:   req.Content,
		Metadata:  req.Metadata,
		Embedding: embedding,
		CreatedAt: time.Now().UTC(),
	}

	// 2. Persist to PostgreSQL if connected
	if h.db != nil {
		if err := pgstore.CreateDocument(ctx, h.db, doc); err != nil {
			response.Error(c, apperrors.NewInternal("Failed to persist document to pgvector", err))
			return
		}
		response.Created(c, gin.H{
			"id":         doc.ID,
			"title":      doc.Title,
			"content":    doc.Content,
			"metadata":   doc.Metadata,
			"dimensions": len(doc.Embedding),
			"created_at": doc.CreatedAt,
		})
		return
	}

	// In-memory fallback
	h.fallbackMu.Lock()
	h.fallbackDocs = append(h.fallbackDocs, doc)
	h.fallbackMu.Unlock()

	response.Created(c, gin.H{
		"id":         doc.ID,
		"title":      doc.Title,
		"content":    doc.Content,
		"metadata":   doc.Metadata,
		"dimensions": len(doc.Embedding),
		"mode":       "in-memory fallback (postgres disconnected)",
		"created_at": doc.CreatedAt,
	})
}

// ListDocuments retrieves stored documents.
func (h *RAGHandler) ListDocuments(c *gin.Context) {
	ctx := c.Request.Context()

	if h.db != nil {
		docs, err := pgstore.ListDocuments(ctx, h.db, 50, 0)
		if err != nil {
			response.Error(c, apperrors.NewInternal("Failed to query documents", err))
			return
		}
		response.OK(c, docs)
		return
	}

	// In-memory fallback
	h.fallbackMu.RLock()
	defer h.fallbackMu.RUnlock()
	response.OK(c, h.fallbackDocs)
}

// SearchDocumentsRequest defines the query payload for semantic vector search.
type SearchDocumentsRequest struct {
	Query string `json:"query" binding:"required"`
	Limit int    `json:"limit"`
}

// SearchDocuments embeds the query and retrieves closest documents via cosine similarity in pgvector.
func (h *RAGHandler) SearchDocuments(c *gin.Context) {
	var req SearchDocumentsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, apperrors.NewBadRequest("Invalid request payload", err.Error()))
		return
	}

	if req.Limit <= 0 {
		req.Limit = 5
	}

	ctx := c.Request.Context()

	// 1. Embed query
	var queryEmbedding []float32
	var err error
	if h.llm != nil {
		queryEmbedding, err = h.llm.GenerateEmbedding(ctx, req.Query)
	} else {
		queryEmbedding = llmclient.GenerateMockEmbedding(req.Query)
	}
	if err != nil {
		response.Error(c, apperrors.NewInternal("Failed to embed search query", err))
		return
	}

	// 2. Query PostgreSQL pgvector
	if h.db != nil {
		results, err := pgstore.SearchSimilarDocuments(ctx, h.db, queryEmbedding, req.Limit)
		if err != nil {
			response.Error(c, apperrors.NewInternal("Vector search failed", err))
			return
		}
		response.OK(c, gin.H{
			"query":   req.Query,
			"matches": results,
			"count":   len(results),
		})
		return
	}

	// In-memory fallback cosine search
	h.fallbackMu.RLock()
	defer h.fallbackMu.RUnlock()

	var matches []*pgstore.DocumentSearchResult
	for _, doc := range h.fallbackDocs {
		sim := computeCosineSimilarity(queryEmbedding, doc.Embedding)
		matches = append(matches, &pgstore.DocumentSearchResult{
			ID:         doc.ID,
			Title:      doc.Title,
			Content:    doc.Content,
			Metadata:   doc.Metadata,
			Similarity: float64(sim),
			CreatedAt:  doc.CreatedAt,
		})
	}

	response.OK(c, gin.H{
		"query":   req.Query,
		"matches": matches,
		"count":   len(matches),
		"mode":    "in-memory fallback (postgres disconnected)",
	})
}

// AskRAGRequest defines payload for question answering over indexed context documents.
type AskRAGRequest struct {
	Question string `json:"question" binding:"required"`
	Model    string `json:"model"`
	Limit    int    `json:"limit"`
}

// AskRAG performs end-to-end Retrieval-Augmented Generation (retrieve context from pgvector, augment prompt, answer with LLM).
func (h *RAGHandler) AskRAG(c *gin.Context) {
	var req AskRAGRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, apperrors.NewBadRequest("Invalid request payload", err.Error()))
		return
	}

	if req.Limit <= 0 {
		req.Limit = 3
	}

	ctx := c.Request.Context()

	// 1. Embed question
	var qEmbedding []float32
	var err error
	if h.llm != nil {
		qEmbedding, err = h.llm.GenerateEmbedding(ctx, req.Question)
	} else {
		qEmbedding = llmclient.GenerateMockEmbedding(req.Question)
	}
	if err != nil {
		response.Error(c, apperrors.NewInternal("Failed to embed question", err))
		return
	}

	// 2. Retrieve top context documents
	var matches []*pgstore.DocumentSearchResult
	if h.db != nil {
		matches, err = pgstore.SearchSimilarDocuments(ctx, h.db, qEmbedding, req.Limit)
		if err != nil {
			response.Error(c, apperrors.NewInternal("Failed to retrieve context documents", err))
			return
		}
	} else {
		h.fallbackMu.RLock()
		for _, doc := range h.fallbackDocs {
			sim := computeCosineSimilarity(qEmbedding, doc.Embedding)
			matches = append(matches, &pgstore.DocumentSearchResult{
				ID:         doc.ID,
				Title:      doc.Title,
				Content:    doc.Content,
				Metadata:   doc.Metadata,
				Similarity: float64(sim),
				CreatedAt:  doc.CreatedAt,
			})
		}
		h.fallbackMu.RUnlock()
	}

	// 3. Assemble Augmented Prompt with Context
	var contextBuilder strings.Builder
	for i, m := range matches {
		contextBuilder.WriteString(fmt.Sprintf("\n[Document %d - %s (Similarity: %.2f%%)]:\n%s\n", i+1, m.Title, m.Similarity*100, m.Content))
	}

	augmentedPrompt := fmt.Sprintf(
		"You are a helpful knowledge assistant. Answer the user's question accurately using ONLY the context provided below. If the answer cannot be found in the context, explicitly explain that.\n\n=== Context Documents ===%s\n\n=== Question ===\n%s\n\n=== Answer ===",
		contextBuilder.String(),
		req.Question,
	)

	// 4. Query LLM
	var answer string
	if h.llm != nil {
		answer, err = h.llm.GenerateChatCompletion(ctx, req.Model, augmentedPrompt)
		if err != nil {
			response.Error(c, apperrors.NewInternal("LLM generation failed", err))
			return
		}
	} else {
		answer = fmt.Sprintf("Based on the %d context document(s) retrieved from pgvector:\n%s\n(Note: Add OPENAI_API_KEY to generate live LLM completions).", len(matches), summarizeMatches(matches))
	}

	response.OK(c, gin.H{
		"question":  req.Question,
		"answer":    answer,
		"citations": matches,
	})
}

func computeCosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float32
	for i := 0; i < len(a); i++ {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (float32(float64(normA)*float64(normB)) + 1e-9)
}

func summarizeMatches(matches []*pgstore.DocumentSearchResult) string {
	if len(matches) == 0 {
		return "No relevant documents found in knowledge base."
	}
	var sb strings.Builder
	for _, m := range matches {
		sb.WriteString(fmt.Sprintf("- %s: %s\n", m.Title, m.Content))
	}
	return sb.String()
}
