package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

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

	// Store upload metadata in Redis for chunked uploads
	// TODO: Store in Redis

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

	// TODO: Assemble chunks into final file
	// For now, just return success

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

	// TODO: Fetch from database
	response := map[string]interface{}{
		"id":       fileID,
		"name":     "example.mp4",
		"size":     1024000,
		"mimeType": "video/mp4",
		"status":   "ready",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// DownloadFile streams a file download
func (h *FileHandler) DownloadFile(w http.ResponseWriter, r *http.Request) {
	fileID := chi.URLParam(r, "id")
	if fileID == "" {
		http.Error(w, "file id required", http.StatusBadRequest)
		return
	}

	// TODO: Get file path from database
	// For now, return placeholder
	http.Error(w, "file not found", http.StatusNotFound)
}

// GetThumbnail returns a thumbnail for media files
func (h *FileHandler) GetThumbnail(w http.ResponseWriter, r *http.Request) {
	fileID := chi.URLParam(r, "id")
	if fileID == "" {
		http.Error(w, "file id required", http.StatusBadRequest)
		return
	}

	// TODO: Generate/retrieve thumbnail
	http.Error(w, "thumbnail not available", http.StatusNotFound)
}

// DeleteFile removes a file
func (h *FileHandler) DeleteFile(w http.ResponseWriter, r *http.Request) {
	fileID := chi.URLParam(r, "id")
	if fileID == "" {
		http.Error(w, "file id required", http.StatusBadRequest)
		return
	}

	// TODO: Delete from storage and database
	h.logger.Info("File deleted", zap.String("file_id", fileID))

	w.WriteHeader(http.StatusNoContent)
}

// SimpleUpload handles direct file upload (for small files)
func (h *FileHandler) SimpleUpload(w http.ResponseWriter, r *http.Request) {
	// Parse multipart form with 32MB max memory
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "failed to parse form", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
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

	h.logger.Info("File uploaded",
		zap.String("file_id", fileInfo.ID),
		zap.String("filename", header.Filename),
		zap.Int64("size", fileInfo.Size),
		zap.String("mime_type", mimeType),
	)

	response := map[string]interface{}{
		"fileId":   fileInfo.ID,
		"name":     header.Filename,
		"size":     fileInfo.Size,
		"mimeType": mimeType,
		"path":     fileInfo.Path,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}
