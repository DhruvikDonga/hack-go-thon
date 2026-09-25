package handler

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"hack-go-thon/pkg/apperrors"
	"hack-go-thon/pkg/log"
	"hack-go-thon/pkg/response"

	"github.com/gin-gonic/gin"
)

// UploadedFile contains metadata for an uploaded asset.
type UploadedFile struct {
	ID            string    `json:"id"`
	OriginalName  string    `json:"original_name"`
	StoredName    string    `json:"stored_name"`
	SizeBytes     int64     `json:"size_bytes"`
	SizeFormatted string    `json:"size_formatted"`
	MimeType      string    `json:"mime_type"`
	Category      string    `json:"category,omitempty"`
	Description   string    `json:"description,omitempty"`
	SHA256        string    `json:"sha256"`
	URL           string    `json:"url"`
	DownloadURL   string    `json:"download_url"`
	UploadedAt    time.Time `json:"uploaded_at"`
}

// UploadHandler processes multipart form file uploads, listing, safe file serving, and deletions.
type UploadHandler struct {
	uploadDir string
	maxSize   int64
	mu        sync.RWMutex
	files     map[string]*UploadedFile
}

// NewUploadHandler initializes the file upload handler and ensures the upload directory exists.
func NewUploadHandler(uploadDir string, maxSize int64) *UploadHandler {
	if uploadDir == "" {
		uploadDir = "./uploads"
	}
	if maxSize <= 0 {
		maxSize = 32 << 20 // 32MB default
	}

	_ = os.MkdirAll(uploadDir, 0755)

	h := &UploadHandler{
		uploadDir: uploadDir,
		maxSize:   maxSize,
		files:     make(map[string]*UploadedFile),
	}

	// Scan upload directory to populate existing files into catalog
	h.scanExistingFiles()
	return h
}

// Upload handles multipart form file upload requests.
// Supports single file ("file") or multiple files ("files" / "file").
// POST /api/v1/upload
func (h *UploadHandler) Upload(c *gin.Context) {
	// Enforce max multipart memory buffer
	if err := c.Request.ParseMultipartForm(h.maxSize); err != nil {
		response.Error(c, apperrors.NewBadRequest(fmt.Sprintf("Failed to parse multipart form or payload exceeds %s: %v", formatBytes(h.maxSize), err)))
		return
	}

	category := strings.TrimSpace(c.PostForm("category"))
	if category == "" {
		category = "general"
	}
	description := strings.TrimSpace(c.PostForm("description"))

	// Collect uploaded files from form: either "file" or "files"
	form := c.Request.MultipartForm
	var fileHeaders []*multipart.FileHeader

	if files, ok := form.File["files"]; ok && len(files) > 0 {
		fileHeaders = append(fileHeaders, files...)
	}
	if files, ok := form.File["file"]; ok && len(files) > 0 {
		fileHeaders = append(fileHeaders, files...)
	}

	if len(fileHeaders) == 0 {
		response.Error(c, apperrors.NewBadRequest("No file uploaded. Expected multipart form field 'file' or 'files'"))
		return
	}

	var results []*UploadedFile
	for _, fh := range fileHeaders {
		if fh.Size > h.maxSize {
			response.Error(c, apperrors.NewBadRequest(fmt.Sprintf("File '%s' exceeds maximum allowed size of %s", fh.Filename, formatBytes(h.maxSize))))
			return
		}

		uploaded, err := h.saveFile(fh, category, description)
		if err != nil {
			response.Error(c, apperrors.NewInternal(fmt.Sprintf("Failed to process file '%s': %v", fh.Filename, err)))
			return
		}
		results = append(results, uploaded)
	}

	log.Info("Successfully uploaded file(s)", "count", len(results), "category", category)

	if len(results) == 1 {
		response.Created(c, results[0])
		return
	}
	response.Created(c, gin.H{
		"count": len(results),
		"files": results,
	})
}

// ListFiles lists metadata for all uploaded assets.
// GET /api/v1/files
func (h *UploadHandler) ListFiles(c *gin.Context) {
	categoryFilter := strings.TrimSpace(c.Query("category"))

	h.mu.RLock()
	list := make([]*UploadedFile, 0, len(h.files))
	for _, f := range h.files {
		if categoryFilter != "" && !strings.EqualFold(f.Category, categoryFilter) {
			continue
		}
		list = append(list, f)
	}
	h.mu.RUnlock()

	response.OK(c, gin.H{
		"total": len(list),
		"files": list,
	})
}

// GetFile serves an uploaded file with path traversal security.
// If ?download=true is provided, Content-Disposition: attachment is applied.
// GET /api/v1/files/:filename
func (h *UploadHandler) GetFile(c *gin.Context) {
	rawFilename := c.Param("filename")
	filename := filepath.Base(filepath.Clean(rawFilename))

	if filename == "." || filename == "/" || strings.Contains(filename, "..") {
		response.Error(c, apperrors.NewBadRequest("Invalid file name requested"))
		return
	}

	filePath := filepath.Join(h.uploadDir, filename)
	fileInfo, err := os.Stat(filePath)
	if os.IsNotExist(err) || (err == nil && fileInfo.IsDir()) {
		response.Error(c, apperrors.NewNotFound("Requested file does not exist"))
		return
	}

	h.mu.RLock()
	meta, hasMeta := h.files[filename]
	h.mu.RUnlock()

	// Apply download attachment disposition if requested
	if c.Query("download") == "true" {
		downloadName := filename
		if hasMeta && meta.OriginalName != "" {
			downloadName = meta.OriginalName
		}
		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, downloadName))
	} else if hasMeta && meta.MimeType != "" {
		c.Header("Content-Type", meta.MimeType)
	}

	c.File(filePath)
}

// DeleteFile removes an uploaded file from disk and the catalog.
// DELETE /api/v1/files/:filename
func (h *UploadHandler) DeleteFile(c *gin.Context) {
	rawFilename := c.Param("filename")
	filename := filepath.Base(filepath.Clean(rawFilename))

	if filename == "." || filename == "/" || strings.Contains(filename, "..") {
		response.Error(c, apperrors.NewBadRequest("Invalid file name"))
		return
	}

	filePath := filepath.Join(h.uploadDir, filename)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		response.Error(c, apperrors.NewNotFound("File not found"))
		return
	}

	if err := os.Remove(filePath); err != nil {
		response.Error(c, apperrors.NewInternal("Failed to delete file from disk: "+err.Error()))
		return
	}

	h.mu.Lock()
	delete(h.files, filename)
	h.mu.Unlock()

	log.Info("Deleted uploaded file", "filename", filename)
	response.OK(c, gin.H{"deleted": true, "filename": filename})
}

// saveFile writes an uploaded multipart file to disk and records metadata.
func (h *UploadHandler) saveFile(fh *multipart.FileHeader, category, description string) (*UploadedFile, error) {
	src, err := fh.Open()
	if err != nil {
		return nil, fmt.Errorf("open file error: %w", err)
	}
	defer src.Close()

	// 1. Sniff MIME type using first 512 bytes
	headerBytes := make([]byte, 512)
	n, _ := src.Read(headerBytes)
	mimeType := http.DetectContentType(headerBytes[:n])
	if headerMime := fh.Header.Get("Content-Type"); headerMime != "" && (mimeType == "application/octet-stream" || mimeType == "") {
		mimeType = headerMime
	}
	// Rewind reader back to beginning
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek rewind error: %w", err)
	}

	// 2. Generate safe stored filename
	origBase := filepath.Base(fh.Filename)
	ext := filepath.Ext(origBase)
	baseWithoutExt := strings.TrimSuffix(origBase, ext)
	sanitizedBase := sanitizeFilename(baseWithoutExt)
	if sanitizedBase == "" {
		sanitizedBase = "file"
	}

	randBytes := make([]byte, 4)
	_, _ = rand.Read(randBytes)
	storedName := fmt.Sprintf("%d_%x_%s%s", time.Now().Unix(), randBytes, sanitizedBase, ext)
	destPath := filepath.Join(h.uploadDir, storedName)

	destFile, err := os.Create(destPath)
	if err != nil {
		return nil, fmt.Errorf("create file error: %w", err)
	}
	defer destFile.Close()

	// 3. Compute SHA256 while writing to disk
	hasher := sha256.New()
	multiWriter := io.MultiWriter(destFile, hasher)

	copiedBytes, err := io.Copy(multiWriter, src)
	if err != nil {
		_ = os.Remove(destPath)
		return nil, fmt.Errorf("copy write error: %w", err)
	}

	sha256Hex := hex.EncodeToString(hasher.Sum(nil))
	fileID := fmt.Sprintf("file_%d_%x", time.Now().Unix(), randBytes)
	now := time.Now().UTC()

	uploaded := &UploadedFile{
		ID:            fileID,
		OriginalName:  origBase,
		StoredName:    storedName,
		SizeBytes:     copiedBytes,
		SizeFormatted: formatBytes(copiedBytes),
		MimeType:      mimeType,
		Category:      category,
		Description:   description,
		SHA256:        sha256Hex,
		URL:           "/api/v1/files/" + storedName,
		DownloadURL:   "/api/v1/files/" + storedName + "?download=true",
		UploadedAt:    now,
	}

	h.mu.Lock()
	h.files[storedName] = uploaded
	h.mu.Unlock()

	return uploaded, nil
}

// scanExistingFiles loads existing files in the directory on startup.
func (h *UploadHandler) scanExistingFiles() {
	entries, err := os.ReadDir(h.uploadDir)
	if err != nil {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		name := entry.Name()
		h.files[name] = &UploadedFile{
			ID:            "file_" + name,
			OriginalName:  name,
			StoredName:    name,
			SizeBytes:     info.Size(),
			SizeFormatted: formatBytes(info.Size()),
			MimeType:      detectMimeFromExt(name),
			Category:      "general",
			URL:           "/api/v1/files/" + name,
			DownloadURL:   "/api/v1/files/" + name + "?download=true",
			UploadedAt:    info.ModTime().UTC(),
		}
	}
}

// sanitizeFilename strips non-alphanumeric characters for safe filename storage.
func sanitizeFilename(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// formatBytes formats byte sizes into human readable units.
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// detectMimeFromExt provides fallback MIME lookup based on file extension.
func detectMimeFromExt(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".pdf":
		return "application/pdf"
	case ".json":
		return "application/json"
	case ".txt":
		return "text/plain"
	case ".mp4":
		return "video/mp4"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	default:
		return "application/octet-stream"
	}
}
