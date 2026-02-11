package middleware

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
)

// FileValidationConfig defines file validation rules
type FileValidationConfig struct {
	MaxSize      int64    // Maximum file size in bytes
	AllowedTypes []string // Allowed MIME types (e.g., "video/mp4", "image/*")
	AllowedExts  []string // Allowed file extensions (e.g., ".mp4", ".jpg")
}

// ValidateFileUpload validates uploaded files
func ValidateFileUpload(config FileValidationConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only validate for multipart/form-data requests
			if !strings.Contains(r.Header.Get("Content-Type"), "multipart/form-data") {
				next.ServeHTTP(w, r)
				return
			}
			
			// Parse multipart form
			err := r.ParseMultipartForm(32 << 20) // 32 MB max memory
			if err != nil {
				http.Error(w, "Failed to parse form", http.StatusBadRequest)
				return
			}
			
			// Validate each uploaded file
			if r.MultipartForm != nil && r.MultipartForm.File != nil {
				for _, fileHeaders := range r.MultipartForm.File {
					for _, fileHeader := range fileHeaders {
						if err := validateFile(fileHeader, config); err != nil {
							http.Error(w, err.Error(), http.StatusBadRequest)
							return
						}
					}
				}
			}
			
			next.ServeHTTP(w, r)
		})
	}
}

// validateFile validates a single file
func validateFile(fileHeader *multipart.FileHeader, config FileValidationConfig) error {
	// Check file size
	if config.MaxSize > 0 && fileHeader.Size > config.MaxSize {
		return fmt.Errorf("file size %d exceeds maximum allowed size %d", fileHeader.Size, config.MaxSize)
	}
	
	// Check file extension
	if len(config.AllowedExts) > 0 {
		ext := strings.ToLower(filepath.Ext(fileHeader.Filename))
		allowed := false
		for _, allowedExt := range config.AllowedExts {
			if ext == strings.ToLower(allowedExt) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("file extension %s is not allowed", ext)
		}
	}
	
	// Check MIME type by reading magic bytes
	if len(config.AllowedTypes) > 0 {
		file, err := fileHeader.Open()
		if err != nil {
			return fmt.Errorf("failed to open file: %w", err)
		}
		defer file.Close()
		
		// Read first 512 bytes for MIME type detection
		buffer := make([]byte, 512)
		n, err := file.Read(buffer)
		if err != nil && err != io.EOF {
			return fmt.Errorf("failed to read file: %w", err)
		}
		
		// Detect content type
		contentType := http.DetectContentType(buffer[:n])
		
		// Check if content type is allowed
		allowed := false
		for _, allowedType := range config.AllowedTypes {
			if matchMIMEType(contentType, allowedType) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("file type %s is not allowed", contentType)
		}
	}
	
	return nil
}

// matchMIMEType checks if a MIME type matches a pattern (supports wildcards)
func matchMIMEType(contentType, pattern string) bool {
	// Exact match
	if contentType == pattern {
		return true
	}
	
	// Wildcard match (e.g., "image/*" matches "image/jpeg")
	if strings.HasSuffix(pattern, "/*") {
		prefix := strings.TrimSuffix(pattern, "/*")
		return strings.HasPrefix(contentType, prefix+"/")
	}
	
	return false
}

// ValidateJSONBody validates that the request body is valid JSON
func ValidateJSONBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" || r.Method == "PUT" || r.Method == "PATCH" {
			contentType := r.Header.Get("Content-Type")
			if strings.Contains(contentType, "application/json") {
				// Read body
				body, err := io.ReadAll(r.Body)
				if err != nil {
					http.Error(w, "Failed to read request body", http.StatusBadRequest)
					return
				}
				defer r.Body.Close()
				
				// Check if body is empty for required content
				if len(body) == 0 {
					http.Error(w, "Request body is required", http.StatusBadRequest)
					return
				}
				
				// Restore body for handlers
				r.Body = io.NopCloser(bytes.NewBuffer(body))
			}
		}
		
		next.ServeHTTP(w, r)
	})
}

// Media file validation configs

// VideoFileValidation validates video files
var VideoFileValidation = FileValidationConfig{
	MaxSize: 5 * 1024 * 1024 * 1024, // 5 GB
	AllowedTypes: []string{
		"video/mp4",
		"video/mpeg",
		"video/quicktime",
		"video/x-msvideo",
		"video/webm",
		"video/x-matroska",
	},
	AllowedExts: []string{
		".mp4", ".mpeg", ".mpg", ".mov", ".avi", ".webm", ".mkv", ".flv", ".wmv",
	},
}

// AudioFileValidation validates audio files
var AudioFileValidation = FileValidationConfig{
	MaxSize: 500 * 1024 * 1024, // 500 MB
	AllowedTypes: []string{
		"audio/mpeg",
		"audio/wav",
		"audio/x-wav",
		"audio/ogg",
		"audio/mp4",
		"audio/aac",
		"audio/flac",
		"audio/x-flac",
	},
	AllowedExts: []string{
		".mp3", ".wav", ".ogg", ".m4a", ".aac", ".flac", ".wma",
	},
}

// ImageFileValidation validates image files
var ImageFileValidation = FileValidationConfig{
	MaxSize: 100 * 1024 * 1024, // 100 MB
	AllowedTypes: []string{
		"image/jpeg",
		"image/png",
		"image/gif",
		"image/webp",
		"image/bmp",
	},
	AllowedExts: []string{
		".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp",
	},
}

// AllMediaFileValidation validates all media types
var AllMediaFileValidation = FileValidationConfig{
	MaxSize: 5 * 1024 * 1024 * 1024, // 5 GB
	AllowedTypes: append(append(
		VideoFileValidation.AllowedTypes,
		AudioFileValidation.AllowedTypes...),
		ImageFileValidation.AllowedTypes...),
	AllowedExts: append(append(
		VideoFileValidation.AllowedExts,
		AudioFileValidation.AllowedExts...),
		ImageFileValidation.AllowedExts...),
}
