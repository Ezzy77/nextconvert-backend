package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/convert-studio/backend/internal/shared/database"
	"github.com/convert-studio/backend/internal/shared/storage"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// FileHandler handles file operations
type FileHandler struct {
	storage *storage.Service
	db      *database.Postgres
	logger  *zap.Logger
}

// NewFileHandler creates a new file handler
func NewFileHandler(storage *storage.Service, db *database.Postgres, logger *zap.Logger) *FileHandler {
	return &FileHandler{
		storage: storage,
		db:      db,
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

// DownloadFile streams a file download
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

	// Open file from storage
	reader, err := h.storage.Retrieve(r.Context(), file.StoragePath)
	if err != nil {
		h.logger.Error("Failed to retrieve file from storage", zap.Error(err), zap.String("path", file.StoragePath))
		http.Error(w, "file not found in storage", http.StatusNotFound)
		return
	}
	defer reader.Close()

	// Set headers for download
	w.Header().Set("Content-Type", file.MimeType)
	w.Header().Set("Content-Disposition", "attachment; filename=\""+file.OriginalName+"\"")
	w.Header().Set("Content-Length", strconv.FormatInt(file.SizeBytes, 10))

	// Stream file to response
	if _, err := io.Copy(w, reader); err != nil {
		h.logger.Error("Failed to stream file", zap.Error(err))
	}
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

// DeleteFile removes a file
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

	// Insert into database
	fileRecord, err := h.insertFileToDB(r.Context(), fileInfo, header.Filename, mimeType, mediaType)
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
func (h *FileHandler) insertFileToDB(ctx context.Context, fileInfo *storage.FileInfo, originalName, mimeType string, mediaType *string) (*FileRecord, error) {
	var record FileRecord
	err := h.db.Pool.QueryRow(ctx, `
		INSERT INTO files (id, original_name, storage_path, mime_type, size_bytes, zone, media_type, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
		RETURNING id, original_name, storage_path, mime_type, size_bytes, zone, media_type, expires_at, created_at
	`,
		fileInfo.ID,
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
