package handler

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestUploadHandler_Flow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tempDir, err := os.MkdirTemp("", "upload-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	maxSize := int64(10 * 1024 * 1024) // 10MB
	h := NewUploadHandler(tempDir, maxSize)

	r := gin.New()
	r.POST("/upload", h.Upload)
	r.GET("/files", h.ListFiles)
	r.GET("/files/:filename", h.GetFile)
	r.DELETE("/files/:filename", h.DeleteFile)

	// 1. Upload single text file
	fileContent := []byte("Hello, this is a test upload file content!")
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", "sample.txt")
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}
	_, _ = part.Write(fileContent)
	_ = writer.WriteField("category", "documents")
	_ = writer.WriteField("description", "A sample text document")
	_ = writer.Close()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
	}

	var uploadResp struct {
		Success bool         `json:"success"`
		Data    UploadedFile `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &uploadResp); err != nil {
		t.Fatalf("failed to parse upload response: %v", err)
	}

	file1 := uploadResp.Data
	if file1.OriginalName != "sample.txt" || file1.Category != "documents" {
		t.Errorf("unexpected file metadata: %+v", file1)
	}
	if file1.SizeBytes != int64(len(fileContent)) {
		t.Errorf("expected size %d, got %d", len(fileContent), file1.SizeBytes)
	}

	// 2. Verify file exists on disk
	storedPath := filepath.Join(tempDir, file1.StoredName)
	if _, err := os.Stat(storedPath); os.IsNotExist(err) {
		t.Fatalf("file not found on disk at %s", storedPath)
	}

	// 3. List uploaded files
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/files", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on list, got %d", w.Code)
	}

	var listResp struct {
		Success bool `json:"success"`
		Data    struct {
			Total int            `json:"total"`
			Files []UploadedFile `json:"files"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("failed to parse list response: %v", err)
	}
	if listResp.Data.Total != 1 {
		t.Fatalf("expected 1 file in catalog, got %d", listResp.Data.Total)
	}

	// 4. Download file with ?download=true
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/files/"+file1.StoredName+"?download=true", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on file fetch, got %d", w.Code)
	}
	if !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") {
		t.Errorf("expected attachment disposition, got %s", w.Header().Get("Content-Disposition"))
	}
	if w.Body.String() != string(fileContent) {
		t.Errorf("downloaded content mismatch, got %s", w.Body.String())
	}

	// 5. Path traversal attempt must be rejected
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/files/../../etc/passwd", nil)
	r.ServeHTTP(w, req)
	if w.Code == http.StatusOK {
		t.Errorf("expected rejection of path traversal, got 200 OK")
	}

	// 6. Delete file
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("DELETE", "/files/"+file1.StoredName, nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on delete, got %d", w.Code)
	}

	if _, err := os.Stat(storedPath); !os.IsNotExist(err) {
		t.Errorf("file should have been deleted from disk")
	}
}

func TestUploadHandler_SizeLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tempDir, err := os.MkdirTemp("", "upload-test-limit-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	maxSize := int64(1024) // 1 KB max
	h := NewUploadHandler(tempDir, maxSize)

	r := gin.New()
	r.POST("/upload", h.Upload)

	// File of 2 KB exceeds 1 KB limit
	oversizedContent := bytes.Repeat([]byte("A"), 2048)
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "oversized.bin")
	_, _ = part.Write(oversizedContent)
	_ = writer.Close()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for oversized file, got %d: %s", w.Code, w.Body.String())
	}
}
