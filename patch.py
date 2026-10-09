import sys

with open('internal/api/handler/upload_handler.go', 'r') as f:
    code = f.read()

import re

# Add imports
imports = """
	"context"
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
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	dbclient "hack-go-thon/internal/db_client"
	"hack-go-thon/internal/store/pg_store"
	"hack-go-thon/pkg/apperrors"
	"hack-go-thon/pkg/log"
	"hack-go-thon/pkg/response"

	"github.com/gin-gonic/gin"
"""

code = re.sub(r'import \((.*?)\)', 'import (' + imports + ')', code, flags=re.DOTALL)

# Update struct
struct_def = """type UploadHandler struct {
	uploadDir string
	maxSize   int64
	db        *dbclient.PostgresDatabase
	s3Client  *s3.Client
	s3Bucket  string
}"""

code = re.sub(r'type UploadHandler struct \{.*?\}', struct_def, code, flags=re.DOTALL)

# Update NewUploadHandler
new_handler = """func NewUploadHandler(uploadDir string, maxSize int64, db *dbclient.PostgresDatabase) *UploadHandler {
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
		db:        db,
	}

	if bucket := os.Getenv("S3_BUCKET"); bucket != "" {
		cfg, err := config.LoadDefaultConfig(context.Background())
		if err == nil {
			h.s3Client = s3.NewFromConfig(cfg)
			h.s3Bucket = bucket
			log.Info("S3 upload configured", "bucket", bucket, "region", cfg.Region)
		} else {
			log.Error("Failed to load AWS config for S3", "error", err)
		}
	}

	return h
}"""

code = re.sub(r'func NewUploadHandler.*?return h\n\}', new_handler, code, flags=re.DOTALL)

# Update DeleteFile
delete_file = """func (h *UploadHandler) DeleteFile(c *gin.Context) {
	rawFilename := c.Param("filename")
	filename := filepath.Base(filepath.Clean(rawFilename))

	if filename == "." || filename == "/" || strings.Contains(filename, "..") {
		response.Error(c, apperrors.NewBadRequest("Invalid file name"))
		return
	}

	if h.s3Client != nil {
		_, err := h.s3Client.DeleteObject(c.Request.Context(), &s3.DeleteObjectInput{
			Bucket: aws.String(h.s3Bucket),
			Key:    aws.String(filename),
		})
		if err != nil {
			log.Error("Failed to delete from S3", "error", err)
		}
	} else {
		filePath := filepath.Join(h.uploadDir, filename)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			response.Error(c, apperrors.NewNotFound("File not found"))
			return
		}

		if err := os.Remove(filePath); err != nil {
			response.Error(c, apperrors.NewInternal("Failed to delete file from disk: "+err.Error()))
			return
		}
	}

	_ = pgstore.DeleteUploadedFile(c.Request.Context(), h.db, filename)

	log.Info("Deleted uploaded file", "filename", filename)
	response.OK(c, gin.H{"deleted": true, "filename": filename})
}"""

code = re.sub(r'func \(h \*UploadHandler\) DeleteFile\(c \*gin\.Context\).*?response\.OK\(c, gin\.H\{"deleted": true, "filename": filename\}\)\n\}', delete_file, code, flags=re.DOTALL)

# Update GetFile
get_file = """func (h *UploadHandler) GetFile(c *gin.Context) {
	rawFilename := c.Param("filename")
	filename := filepath.Base(filepath.Clean(rawFilename))

	if filename == "." || filename == "/" || strings.Contains(filename, "..") {
		response.Error(c, apperrors.NewBadRequest("Invalid file name requested"))
		return
	}

	meta, err := pgstore.GetUploadedFileByStoredName(c.Request.Context(), h.db, filename)
	hasMeta := (err == nil && meta != nil)

	if h.s3Client != nil {
		presignClient := s3.NewPresignClient(h.s3Client)
		req, err := presignClient.PresignGetObject(c.Request.Context(), &s3.GetObjectInput{
			Bucket: aws.String(h.s3Bucket),
			Key:    aws.String(filename),
		}, s3.WithPresignExpires(15*time.Minute))
		if err != nil {
			response.Error(c, apperrors.NewInternal("Failed to generate S3 URL"))
			return
		}
		c.Redirect(http.StatusFound, req.URL)
		return
	}

	filePath := filepath.Join(h.uploadDir, filename)
	fileInfo, err := os.Stat(filePath)
	if os.IsNotExist(err) || (err == nil && fileInfo.IsDir()) {
		response.Error(c, apperrors.NewNotFound("Requested file does not exist"))
		return
	}

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
}"""

code = re.sub(r'func \(h \*UploadHandler\) GetFile\(c \*gin\.Context\).*?c\.File\(filePath\)\n\}', get_file, code, flags=re.DOTALL)

# Update saveFile
save_file = """func (h *UploadHandler) saveFile(fh *multipart.FileHeader, category, description string) (*UploadedFile, error) {
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
	
	// 3. Compute SHA256 while writing to disk or S3
	hasher := sha256.New()
	var copiedBytes int64

	if h.s3Client != nil {
		tee := io.TeeReader(src, hasher)
		_, err = h.s3Client.PutObject(context.Background(), &s3.PutObjectInput{
			Bucket:      aws.String(h.s3Bucket),
			Key:         aws.String(storedName),
			Body:        tee,
			ContentType: aws.String(mimeType),
		})
		if err != nil {
			return nil, fmt.Errorf("s3 upload error: %w", err)
		}
		copiedBytes = fh.Size
	} else {
		destPath := filepath.Join(h.uploadDir, storedName)
		destFile, err := os.Create(destPath)
		if err != nil {
			return nil, fmt.Errorf("create file error: %w", err)
		}
		defer destFile.Close()

		multiWriter := io.MultiWriter(destFile, hasher)
		copiedBytes, err = io.Copy(multiWriter, src)
		if err != nil {
			_ = os.Remove(destPath)
			return nil, fmt.Errorf("copy write error: %w", err)
		}
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

	dbModel := &pgstore.UploadedFileModel{
		ID:            fileID,
		OriginalName:  origBase,
		StoredName:    storedName,
		SizeBytes:     copiedBytes,
		SizeFormatted: formatBytes(copiedBytes),
		MimeType:      mimeType,
		Category:      category,
		Description:   description,
		SHA256:        sha256Hex,
		URL:           uploaded.URL,
		DownloadURL:   uploaded.DownloadURL,
		UploadedAt:    now,
	}

	if err := pgstore.InsertUploadedFile(context.Background(), h.db, dbModel); err != nil {
		log.Error("Failed to save file metadata to DB", "error", err)
	}

	return uploaded, nil
}"""

code = re.sub(r'func \(h \*UploadHandler\) saveFile\(fh \*multipart\.FileHeader, category, description string\) \(\*UploadedFile, error\) \{.*?return uploaded, nil\n\}', save_file, code, flags=re.DOTALL)

with open('internal/api/handler/upload_handler.go', 'w') as f:
    f.write(code)

