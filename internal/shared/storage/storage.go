package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nextconvert/backend/internal/shared/config"
	"github.com/google/uuid"
)

// Zone represents a storage zone
type Zone string

const (
	ZoneUpload  Zone = "upload"
	ZoneWorking Zone = "working"
	ZoneOutput  Zone = "output"
)

// FileInfo represents metadata about a stored file
type FileInfo struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Zone      Zone      `json:"zone"`
	Size      int64     `json:"size"`
	MimeType  string    `json:"mime_type"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Service provides file storage operations
type Service struct {
	backend  Backend
	basePath string
	isRemote bool
}

// Backend defines the storage backend interface
type Backend interface {
	Store(ctx context.Context, zone Zone, filename string, reader io.Reader) (string, error)
	Retrieve(ctx context.Context, path string) (io.ReadCloser, error)
	Delete(ctx context.Context, path string) error
	Exists(ctx context.Context, path string) (bool, error)
	GetSize(ctx context.Context, path string) (int64, error)
	List(ctx context.Context, prefix string) ([]string, error)
}

// NewService creates a new storage service
func NewService(cfg config.StorageConfig) (*Service, error) {
	var backend Backend
	var err error

	switch cfg.Backend {
	case "local":
		backend, err = NewLocalBackend(cfg.BasePath)
	case "s3":
		backend, err = NewS3Backend(cfg)
	default:
		backend, err = NewLocalBackend(cfg.BasePath)
	}

	if err != nil {
		return nil, err
	}

	return &Service{
		backend:  backend,
		basePath: cfg.BasePath,
		isRemote: cfg.Backend == "s3",
	}, nil
}

// Store saves a file to the specified zone
func (s *Service) Store(ctx context.Context, zone Zone, originalName string, reader io.Reader) (*FileInfo, error) {
	fileID := uuid.New().String()
	ext := filepath.Ext(originalName)
	filename := fileID + ext

	path, err := s.backend.Store(ctx, zone, filename, reader)
	if err != nil {
		return nil, fmt.Errorf("failed to store file: %w", err)
	}

	size, err := s.backend.GetSize(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("failed to get file size: %w", err)
	}

	// All files expire after 24 hours for security and privacy
	expiresAt := time.Now().Add(24 * time.Hour)

	return &FileInfo{
		ID:        fileID,
		Name:      originalName,
		Path:      path,
		Zone:      zone,
		Size:      size,
		CreatedAt: time.Now(),
		ExpiresAt: expiresAt,
	}, nil
}

// Retrieve gets a file from storage
func (s *Service) Retrieve(ctx context.Context, path string) (io.ReadCloser, error) {
	return s.backend.Retrieve(ctx, path)
}

// Delete removes a file from storage
func (s *Service) Delete(ctx context.Context, path string) error {
	return s.backend.Delete(ctx, path)
}

// Exists checks if a file exists
func (s *Service) Exists(ctx context.Context, path string) (bool, error) {
	return s.backend.Exists(ctx, path)
}

// GetSize returns the size of a file in bytes
func (s *Service) GetSize(ctx context.Context, path string) (int64, error) {
	return s.backend.GetSize(ctx, path)
}

// Copy copies a file from one path to another
func (s *Service) Copy(ctx context.Context, srcPath string, destZone Zone, destName string) (*FileInfo, error) {
	reader, err := s.backend.Retrieve(ctx, srcPath)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	return s.Store(ctx, destZone, destName, reader)
}

// Move moves a file from one zone to another
func (s *Service) Move(ctx context.Context, srcPath string, destZone Zone, destName string) (*FileInfo, error) {
	info, err := s.Copy(ctx, srcPath, destZone, destName)
	if err != nil {
		return nil, err
	}

	if err := s.backend.Delete(ctx, srcPath); err != nil {
		// Log but don't fail
		fmt.Printf("Warning: failed to delete source file after move: %v\n", err)
	}

	return info, nil
}

// GetPath returns the path for a file in a zone
// For local: full filesystem path. For S3: object key (zone/filename)
func (s *Service) GetPath(zone Zone, filename string) string {
	if s.isRemote {
		return filepath.Join(string(zone), filename)
	}
	return filepath.Join(s.basePath, string(zone), filename)
}

// GetFullPath returns the path for local/FFmpeg access
func (s *Service) GetFullPath(storagePath string) string {
	return storagePath
}

// IsRemote returns true if the storage backend is remote (S3)
func (s *Service) IsRemote() bool {
	return s.isRemote
}

// GetPresignedUploadURL generates a presigned PUT URL for direct client-to-S3 upload.
// Returns the presigned URL, the S3 object key, and an error.
func (s *Service) GetPresignedUploadURL(ctx context.Context, zone Zone, fileID string, ext string, contentType string) (string, string, error) {
	s3backend, ok := s.backend.(*S3Backend)
	if !ok {
		return "", "", fmt.Errorf("presigned URLs only supported with S3 backend")
	}

	filename := fileID + ext
	url, key, err := s3backend.GetPresignedUploadURL(ctx, zone, filename, contentType, 15*time.Minute)
	if err != nil {
		return "", "", err
	}
	return url, key, nil
}

// PrepareInputForProcessing downloads remote input to temp for FFmpeg.
// For local backend, returns path as-is and no-op cleanup.
func (s *Service) PrepareInputForProcessing(ctx context.Context, storagePath string) (localPath string, cleanup func(), err error) {
	if !s.isRemote {
		return storagePath, func() {}, nil
	}
	reader, err := s.backend.Retrieve(ctx, storagePath)
	if err != nil {
		return "", nil, err
	}
	defer reader.Close()

	tmpFile, err := os.CreateTemp("", "conv-*"+filepath.Ext(storagePath))
	if err != nil {
		return "", nil, err
	}
	if _, err := io.Copy(tmpFile, reader); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return "", nil, err
	}
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpFile.Name())
		return "", nil, err
	}
	return tmpFile.Name(), func() { os.Remove(tmpFile.Name()) }, nil
}

// FinalizeOutputFromLocal uploads local file to remote at storagePath.
// For local backend, no-op (file already at storagePath).
func (s *Service) FinalizeOutputFromLocal(ctx context.Context, storagePath, localPath string) error {
	if !s.isRemote {
		return nil
	}
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	// Parse storagePath as zone/filename for remote
	parts := strings.SplitN(storagePath, "/", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid storage path: %s", storagePath)
	}
	zone := Zone(parts[0])
	filename := parts[1]

	_, err = s.backend.Store(ctx, zone, filename, f)
	return err
}

// LocalBackend implements local filesystem storage
type LocalBackend struct {
	basePath string
}

// NewLocalBackend creates a new local storage backend
func NewLocalBackend(basePath string) (*LocalBackend, error) {
	// Ensure base directories exist
	for _, zone := range []Zone{ZoneUpload, ZoneWorking, ZoneOutput} {
		path := filepath.Join(basePath, string(zone))
		if err := os.MkdirAll(path, 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory %s: %w", path, err)
		}
	}

	return &LocalBackend{basePath: basePath}, nil
}

func (b *LocalBackend) Store(ctx context.Context, zone Zone, filename string, reader io.Reader) (string, error) {
	path := filepath.Join(b.basePath, string(zone), filename)

	file, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	if _, err := io.Copy(file, reader); err != nil {
		os.Remove(path)
		return "", err
	}

	return path, nil
}

func (b *LocalBackend) Retrieve(ctx context.Context, path string) (io.ReadCloser, error) {
	return os.Open(path)
}

func (b *LocalBackend) Delete(ctx context.Context, path string) error {
	return os.Remove(path)
}

func (b *LocalBackend) Exists(ctx context.Context, path string) (bool, error) {
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

func (b *LocalBackend) GetSize(ctx context.Context, path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func (b *LocalBackend) List(ctx context.Context, prefix string) ([]string, error) {
	var files []string
	err := filepath.Walk(prefix, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

