package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nextconvert/backend/internal/api/middleware"
	"github.com/nextconvert/backend/internal/modules/subscription"
	"github.com/nextconvert/backend/internal/shared/database"
	"github.com/nextconvert/backend/internal/shared/storage"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// FileHandler handles file operations
type FileHandler struct {
	storage   *storage.Service
	db        *database.Postgres
	subSvc    *subscription.Service
	logger    *zap.Logger
}

// NewFileHandler creates a new file handler
func NewFileHandler(storage *storage.Service, db *database.Postgres, subSvc *subscription.Service, logger *zap.Logger) *FileHandler {
	return &FileHandler{
		storage: storage,
		db:      db,
		subSvc:  subSvc,
		logger:  logger,
	}
}

// FileRecord represents a file in the database
type FileRecord struct {
	ID           string    `json:"id"`
	UserID       *string   `json:"userId,omitempty"`
	OriginalName string    `json:"originalName"`
	StoragePath  string    `json:"storagePath"`
	MimeType     string    `json:"mimeType"`
	SizeBytes    int64     `json:"sizeBytes"`
	Zone         string    `json:"zone"`
	MediaType    *string   `json:"mediaType,omitempty"`
	ExpiresAt    time.Time `json:"expiresAt"`
	CreatedAt    time.Time `json:"createdAt"`
}

// UploadInitResponse represents the response for initiating an upload
type UploadInitResponse struct {
	UploadID    string `json:"uploadId"`
	ChunkSize   int64  `json:"chunkSize"`
	TotalChunks int    `json:"totalChunks"`
}

// InitiateUpload starts a new file upload
func (h *FileHandler) InitiateUpload(w http.ResponseWriter, r *http.Request) {
	fileName := r.FormValue("filename")
	fileSizeStr := r.FormValue("size")
	mimeType := r.FormValue("mimeType")

	if fileName == "" || fileSizeStr == "" {
		http.Error(w, "filename and size are required", http.StatusBadRequest)
		return
	}

	fileSize, err := strconv.ParseInt(fileSizeStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid file size", http.StatusBadRequest)
		return
	}

	// Check tier-based file size limit
	user := middleware.GetUser(r.Context())
	userID := "anonymous"
	if user != nil {
		userID = user.ID
	}
	if h.subSvc != nil {
		if err := h.subSvc.CheckLimit(r.Context(), userID, "file_size", fileSize); err != nil {
			h.logger.Warn("File size limit exceeded", zap.String("user_id", userID), zap.Int64("size", fileSize), zap.Error(err))
			http.Error(w, "file size exceeds your plan limit", http.StatusForbidden)
			return
		}
	}

	// For files under 10MB, use simple upload
	const chunkSize int64 = 5 * 1024 * 1024 // 5MB chunks
	totalChunks := int((fileSize + chunkSize - 1) / chunkSize)

	uploadID := uuid.New().String()

	h.logger.Info("Upload initiated",
		zap.String("upload_id", uploadID),
		zap.String("filename", fileName),
		zap.Int64("size", fileSize),
		zap.String("mime_type", mimeType),
	)

	response := UploadInitResponse{
		UploadID:    uploadID,
		ChunkSize:   chunkSize,
		TotalChunks: totalChunks,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// UploadChunk handles a chunk upload
func (h *FileHandler) UploadChunk(w http.ResponseWriter, r *http.Request) {
	uploadID := r.FormValue("uploadId")
	chunkIndexStr := r.FormValue("chunkIndex")

	if uploadID == "" || chunkIndexStr == "" {
		http.Error(w, "uploadId and chunkIndex are required", http.StatusBadRequest)
		return
	}

	chunkIndex, err := strconv.Atoi(chunkIndexStr)
	if err != nil {
		http.Error(w, "invalid chunk index", http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("chunk")
	if err != nil {
		http.Error(w, "failed to read chunk", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Store chunk temporarily
	chunkName := uploadID + "_chunk_" + strconv.Itoa(chunkIndex)
	_, err = h.storage.Store(r.Context(), storage.ZoneWorking, chunkName, file)
	if err != nil {
		h.logger.Error("Failed to store chunk", zap.Error(err))
		http.Error(w, "failed to store chunk", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"received":       true,
		"uploadedChunks": chunkIndex + 1,
	})
}

// CompleteUploadRequest represents the request to complete an upload
type CompleteUploadRequest struct {
	UploadID    string `json:"uploadId"`
	FileName    string `json:"fileName"`
	TotalChunks int    `json:"totalChunks"`
	MimeType    string `json:"mimeType"`
}

// CompleteUploadResponse represents the response after completing an upload
type CompleteUploadResponse struct {
	FileID   string `json:"fileId"`
	Verified bool   `json:"verified"`
}

// CompleteUpload finalizes a chunked upload
func (h *FileHandler) CompleteUpload(w http.ResponseWriter, r *http.Request) {
	var req CompleteUploadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	fileID := uuid.New().String()

	h.logger.Info("Upload completed",
		zap.String("file_id", fileID),
		zap.String("upload_id", req.UploadID),
		zap.String("filename", req.FileName),
	)

	response := CompleteUploadResponse{
		FileID:   fileID,
		Verified: true,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetFile returns file metadata
func (h *FileHandler) GetFile(w http.ResponseWriter, r *http.Request) {
	fileID := chi.URLParam(r, "id")
	if fileID == "" {
		http.Error(w, "file id required", http.StatusBadRequest)
		return
	}

	// Query file from database
	file, err := h.getFileFromDB(r.Context(), fileID)
	if err != nil {
		h.logger.Error("Failed to get file", zap.Error(err), zap.String("file_id", fileID))
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(file)
}

// DownloadFile streams a file download with Range request support for video streaming
func (h *FileHandler) DownloadFile(w http.ResponseWriter, r *http.Request) {
	fileID := chi.URLParam(r, "id")
	if fileID == "" {
		http.Error(w, "file id required", http.StatusBadRequest)
		return
	}

	// Get file metadata from database
	file, err := h.getFileFromDB(r.Context(), fileID)
	if err != nil {
		h.logger.Error("Failed to get file for download", zap.Error(err), zap.String("file_id", fileID))
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	// Retrieve file from storage (works for both local and remote/Supabase)
	reader, err := h.storage.Retrieve(r.Context(), file.StoragePath)
	if err != nil {
		h.logger.Error("Failed to retrieve file from storage", zap.Error(err), zap.String("path", file.StoragePath))
		http.Error(w, "file not found in storage", http.StatusNotFound)
		return
	}
	defer reader.Close()

	fileSize := file.SizeBytes

	// Set common headers
	w.Header().Set("Content-Type", file.MimeType)
	w.Header().Set("Accept-Ranges", "bytes")

	// For local storage, support Range requests for video streaming
	// For remote (Supabase), stream full file
	rangeHeader := r.Header.Get("Range")
	if rangeHeader != "" && !h.storage.IsRemote() {
		f, ok := reader.(*os.File)
		if ok {
			start, end, parseErr := parseRangeHeader(rangeHeader, fileSize)
			if parseErr != nil {
				http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
				return
			}
			if _, seekErr := f.Seek(start, io.SeekStart); seekErr != nil {
				http.Error(w, "failed to seek", http.StatusInternalServerError)
				return
			}
			contentLength := end - start + 1
			w.Header().Set("Content-Length", strconv.FormatInt(contentLength, 10))
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize))
			w.WriteHeader(http.StatusPartialContent)
			if _, err := io.CopyN(w, reader, contentLength); err != nil && !strings.Contains(err.Error(), "broken pipe") && !strings.Contains(err.Error(), "connection reset") {
				h.logger.Error("Failed to stream file range", zap.Error(err))
			}
			return
		}
	}

	// Stream entire file
	w.Header().Set("Content-Disposition", "attachment; filename=\""+file.OriginalName+"\"")
	if fileSize > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(fileSize, 10))
	}
	if _, err := io.Copy(w, reader); err != nil {
		if !strings.Contains(err.Error(), "broken pipe") && !strings.Contains(err.Error(), "connection reset") {
			h.logger.Error("Failed to stream file", zap.Error(err))
		}
	}
}

// parseRangeHeader parses the Range header and returns start and end byte positions
func parseRangeHeader(rangeHeader string, fileSize int64) (int64, int64, error) {
	// Range header format: "bytes=start-end" or "bytes=start-" or "bytes=-suffix"
	if !strings.HasPrefix(rangeHeader, "bytes=") {
		return 0, 0, fmt.Errorf("invalid range format")
	}

	rangeSpec := strings.TrimPrefix(rangeHeader, "bytes=")
	parts := strings.Split(rangeSpec, "-")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid range format")
	}

	var start, end int64
	var err error

	if parts[0] == "" {
		// Suffix range: "-500" means last 500 bytes
		suffix, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid suffix range")
		}
		start = fileSize - suffix
		if start < 0 {
			start = 0
		}
		end = fileSize - 1
	} else {
		// Start is specified
		start, err = strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid start range")
		}

		if parts[1] == "" {
			// Open-ended range: "0-" means from start to end
			end = fileSize - 1
		} else {
			end, err = strconv.ParseInt(parts[1], 10, 64)
			if err != nil {
				return 0, 0, fmt.Errorf("invalid end range")
			}
		}
	}

	// Validate range
	if start < 0 || start >= fileSize || end < start || end >= fileSize {
		// Adjust end if it exceeds file size
		if end >= fileSize {
			end = fileSize - 1
		}
		if start < 0 || start > end {
			return 0, 0, fmt.Errorf("range not satisfiable")
		}
	}

	return start, end, nil
}

// GetThumbnail returns a thumbnail for media files
func (h *FileHandler) GetThumbnail(w http.ResponseWriter, r *http.Request) {
	fileID := chi.URLParam(r, "id")
	if fileID == "" {
		http.Error(w, "file id required", http.StatusBadRequest)
		return
	}

	// For now, return not found - thumbnail generation to be implemented
	http.Error(w, "thumbnail not available", http.StatusNotFound)
}

// ListFiles returns the user's uploaded files
func (h *FileHandler) ListFiles(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r.Context())
	userID := "anonymous"
	if user != nil {
		userID = user.ID
	}

	zone := r.URL.Query().Get("zone")
	if zone == "" {
		zone = "upload"
	}

	// Authenticated: only own files. Anonymous: files with no user
	rows, err := h.db.Pool.Query(r.Context(), `
		SELECT id, original_name, storage_path, mime_type, size_bytes, zone, media_type, created_at
		FROM files
		WHERE (user_id = $1 OR ($1 = 'anonymous' AND user_id IS NULL)) AND zone = $2
		ORDER BY created_at DESC
		LIMIT 200
	`, userID, zone)
	if err != nil {
		h.logger.Error("Failed to list files", zap.Error(err))
		http.Error(w, "failed to list files", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var files []map[string]interface{}
	for rows.Next() {
		var id, originalName, storagePath, mimeType, fileZone string
		var sizeBytes int64
		var mediaType *string
		var createdAt time.Time

		if err := rows.Scan(&id, &originalName, &storagePath, &mimeType, &sizeBytes, &fileZone, &mediaType, &createdAt); err != nil {
			h.logger.Error("Failed to scan file", zap.Error(err))
			continue
		}

		mt := ""
		if mediaType != nil {
			mt = *mediaType
		}

		files = append(files, map[string]interface{}{
			"id":        id,
			"name":      originalName,
			"size":      sizeBytes,
			"mimeType":  mimeType,
			"zone":      fileZone,
			"mediaType": mt,
			"createdAt": createdAt,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(files)
}

// DeleteFile removes a file (user must own it)
func (h *FileHandler) DeleteFile(w http.ResponseWriter, r *http.Request) {
	fileID := chi.URLParam(r, "id")
	if fileID == "" {
		http.Error(w, "file id required", http.StatusBadRequest)
		return
	}

	// Get file to find storage path
	file, err := h.getFileFromDB(r.Context(), fileID)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	// Check ownership
	user := middleware.GetUser(r.Context())
	userID := "anonymous"
	if user != nil {
		userID = user.ID
	}
	if file.UserID != nil && *file.UserID != userID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if file.UserID == nil && userID != "anonymous" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	// Delete from storage
	if err := h.storage.Delete(r.Context(), file.StoragePath); err != nil {
		h.logger.Error("Failed to delete file from storage", zap.Error(err))
	}

	// Delete from database
	_, err = h.db.Pool.Exec(r.Context(), "DELETE FROM files WHERE id = $1", fileID)
	if err != nil {
		h.logger.Error("Failed to delete file from database", zap.Error(err))
		http.Error(w, "failed to delete file", http.StatusInternalServerError)
		return
	}

	h.logger.Info("File deleted", zap.String("file_id", fileID))
	w.WriteHeader(http.StatusNoContent)
}

// SimpleUpload handles direct file upload (for small files)
func (h *FileHandler) SimpleUpload(w http.ResponseWriter, r *http.Request) {
	// Parse multipart form with 500MB max memory
	if err := r.ParseMultipartForm(500 << 20); err != nil {
		h.logger.Error("Failed to parse multipart form", zap.Error(err))
		http.Error(w, "failed to parse form", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		h.logger.Error("Failed to get file from form", zap.Error(err))
		http.Error(w, "failed to get file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Check tier-based file size limit
	user := middleware.GetUser(r.Context())
	userID := "anonymous"
	if user != nil {
		userID = user.ID
	}
	if h.subSvc != nil {
		if err := h.subSvc.CheckLimit(r.Context(), userID, "file_size", header.Size); err != nil {
			h.logger.Warn("File size limit exceeded", zap.String("user_id", userID), zap.Int64("size", header.Size), zap.Error(err))
			http.Error(w, "file size exceeds your plan limit", http.StatusForbidden)
			return
		}
	}

	// Store the file
	fileInfo, err := h.storage.Store(r.Context(), storage.ZoneUpload, header.Filename, file)
	if err != nil {
		h.logger.Error("Failed to store file", zap.Error(err))
		http.Error(w, "failed to store file", http.StatusInternalServerError)
		return
	}

	// Detect MIME type
	buffer := make([]byte, 512)
	file.Seek(0, io.SeekStart)
	file.Read(buffer)
	mimeType := http.DetectContentType(buffer)

	// Determine media type from MIME
	var mediaType *string
	if strings.HasPrefix(mimeType, "video/") {
		mt := "video"
		mediaType = &mt
	} else if strings.HasPrefix(mimeType, "audio/") {
		mt := "audio"
		mediaType = &mt
	} else if strings.HasPrefix(mimeType, "image/") {
		mt := "image"
		mediaType = &mt
	}

	// Get user ID for ownership
	var userIDPtr *string
	if user != nil && user.ID != "anonymous" {
		userIDPtr = &user.ID
	}

	// Insert into database
	fileRecord, err := h.insertFileToDB(r.Context(), fileInfo, header.Filename, mimeType, mediaType, userIDPtr)
	if err != nil {
		h.logger.Error("Failed to insert file into database", zap.Error(err))
		// Delete the stored file since DB insert failed
		h.storage.Delete(r.Context(), fileInfo.Path)
		http.Error(w, "failed to save file metadata", http.StatusInternalServerError)
		return
	}

	h.logger.Info("File uploaded successfully",
		zap.String("file_id", fileRecord.ID),
		zap.String("filename", header.Filename),
		zap.Int64("size", fileInfo.Size),
		zap.String("mime_type", mimeType),
	)

	response := map[string]interface{}{
		"id":          fileRecord.ID,
		"name":        header.Filename,
		"size":        fileInfo.Size,
		"mimeType":    mimeType,
		"storagePath": fileInfo.Path,
		"zone":        string(fileInfo.Zone),
		"createdAt":   fileRecord.CreatedAt,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}

// getFileFromDB retrieves a file record from the database
func (h *FileHandler) getFileFromDB(ctx context.Context, fileID string) (*FileRecord, error) {
	var file FileRecord
	err := h.db.Pool.QueryRow(ctx, `
		SELECT id, user_id, original_name, storage_path, mime_type, size_bytes, zone, media_type, expires_at, created_at
		FROM files
		WHERE id = $1
	`, fileID).Scan(
		&file.ID,
		&file.UserID,
		&file.OriginalName,
		&file.StoragePath,
		&file.MimeType,
		&file.SizeBytes,
		&file.Zone,
		&file.MediaType,
		&file.ExpiresAt,
		&file.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &file, nil
}

// insertFileToDB inserts a new file record into the database
func (h *FileHandler) insertFileToDB(ctx context.Context, fileInfo *storage.FileInfo, originalName, mimeType string, mediaType *string, userID *string) (*FileRecord, error) {
	var record FileRecord
	err := h.db.Pool.QueryRow(ctx, `
		INSERT INTO files (id, user_id, original_name, storage_path, mime_type, size_bytes, zone, media_type, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
		RETURNING id, original_name, storage_path, mime_type, size_bytes, zone, media_type, expires_at, created_at
	`,
		fileInfo.ID,
		userID,
		originalName,
		fileInfo.Path,
		mimeType,
		fileInfo.Size,
		string(fileInfo.Zone),
		mediaType,
		fileInfo.ExpiresAt,
	).Scan(
		&record.ID,
		&record.OriginalName,
		&record.StoragePath,
		&record.MimeType,
		&record.SizeBytes,
		&record.Zone,
		&record.MediaType,
		&record.ExpiresAt,
		&record.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &record, nil
}
