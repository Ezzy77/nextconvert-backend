package storage

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/nextconvert/backend/internal/shared/config"
)

const (
	// Supabase standard upload limit ~50MB; use resumable for larger files
	resumableThreshold = 5 * 1024 * 1024 // 5MB
	tusChunkSize       = 6 * 1024 * 1024 // 6MB - required by Supabase
)

// SupabaseBackend implements storage using Supabase Storage API
type SupabaseBackend struct {
	baseURL    string
	storageURL string // Direct storage host for TUS (project.storage.supabase.co)
	apiKey     string
	bucket     string
	client     *http.Client
}

// NewSupabaseBackend creates a new Supabase storage backend
func NewSupabaseBackend(cfg config.StorageConfig) (*SupabaseBackend, error) {
	url := strings.TrimSuffix(cfg.SupabaseURL, "/")
	if url == "" {
		return nil, fmt.Errorf("SUPABASE_URL is required for supabase storage backend")
	}
	if cfg.SupabaseServiceKey == "" {
		return nil, fmt.Errorf("SUPABASE_SERVICE_ROLE_KEY is required for supabase storage backend")
	}
	bucket := cfg.SupabaseBucket
	if bucket == "" {
		bucket = "media"
	}
	// Direct storage host for TUS (avoids 413, better for large files)
	// https://project.supabase.co -> https://project.storage.supabase.co
	storageURL := url
	if idx := strings.Index(url, ".supabase.co"); idx > 0 {
		storageURL = url[:idx] + ".storage.supabase.co"
	}
	// Long timeout for large video uploads/downloads (default 15 min)
	timeout := 15 * time.Minute
	if cfg.SupabaseTimeout > 0 {
		timeout = time.Duration(cfg.SupabaseTimeout) * time.Second
	}
	return &SupabaseBackend{
		baseURL:    url,
		storageURL: storageURL,
		apiKey:     cfg.SupabaseServiceKey,
		bucket:     bucket,
		client:     &http.Client{Timeout: timeout},
	}, nil
}

// objectPath returns the storage object path (zone/filename)
func (b *SupabaseBackend) objectPath(zone Zone, filename string) string {
	return path.Join(string(zone), filename)
}

// apiURL returns the full API URL for an object
func (b *SupabaseBackend) apiURL(objectPath string, public bool) string {
	ep := "object"
	if public {
		ep = "object/public"
	}
	return fmt.Sprintf("%s/storage/v1/%s/%s/%s", b.baseURL, ep, b.bucket, objectPath)
}

// doRequest executes an HTTP request with auth
func (b *SupabaseBackend) doRequest(ctx context.Context, method, url string, body io.Reader, contentType string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+b.apiKey)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (b *SupabaseBackend) Store(ctx context.Context, zone Zone, filename string, reader io.Reader) (string, error) {
	objectPath := b.objectPath(zone, filename)
	data, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}

	// Use TUS resumable upload for files > 5MB to avoid 413 Payload Too Large
	if int64(len(data)) > resumableThreshold {
		return b.storeResumable(ctx, objectPath, data)
	}

	// Standard upload for small files
	url := fmt.Sprintf("%s/storage/v1/object/%s/%s", b.baseURL, b.bucket, objectPath)
	resp, err := b.doRequest(ctx, "POST", url, bytes.NewReader(data), "application/octet-stream")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("supabase upload failed: %d %s", resp.StatusCode, string(body))
	}

	return objectPath, nil
}

// storeResumable uploads via TUS protocol (chunked, avoids 413 for large files)
func (b *SupabaseBackend) storeResumable(ctx context.Context, objectPath string, data []byte) (string, error) {
	tusEndpoint := b.storageURL + "/storage/v1/upload/resumable"

	// Upload-Metadata: bucketName <base64>, objectName <base64>
	meta := fmt.Sprintf("bucketName %s,objectName %s",
		base64.StdEncoding.EncodeToString([]byte(b.bucket)),
		base64.StdEncoding.EncodeToString([]byte(objectPath)))

	// 1. Create upload
	req, err := http.NewRequestWithContext(ctx, "POST", tusEndpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+b.apiKey)
	req.Header.Set("Tus-Resumable", "1.0.0")
	req.Header.Set("Upload-Length", fmt.Sprintf("%d", len(data)))
	req.Header.Set("Upload-Metadata", meta)
	req.Header.Set("x-upsert", "true")

	resp, err := b.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("supabase tus create failed: %d %s", resp.StatusCode, string(body))
	}

	uploadURL := resp.Header.Get("Location")
	if uploadURL == "" {
		return "", fmt.Errorf("supabase tus create: no Location header")
	}
	// Location may be relative
	if strings.HasPrefix(uploadURL, "/") {
		uploadURL = b.storageURL + uploadURL
	}

	// 2. Upload in 6MB chunks
	for offset := 0; offset < len(data); {
		end := offset + tusChunkSize
		if end > len(data) {
			end = len(data)
		}
		chunk := data[offset:end]

		req, err = http.NewRequestWithContext(ctx, "PATCH", uploadURL, bytes.NewReader(chunk))
		if err != nil {
			return "", err
		}
		req.Header.Set("Authorization", "Bearer "+b.apiKey)
		req.Header.Set("Tus-Resumable", "1.0.0")
		req.Header.Set("Content-Type", "application/offset+octet-stream")
		req.Header.Set("Upload-Offset", fmt.Sprintf("%d", offset))

		resp, err = b.client.Do(req)
		if err != nil {
			return "", err
		}

		if resp.StatusCode != 204 {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return "", fmt.Errorf("supabase tus patch failed: %d %s", resp.StatusCode, string(body))
		}
		resp.Body.Close()

		offset = end
	}

	return objectPath, nil
}

func (b *SupabaseBackend) Retrieve(ctx context.Context, storagePath string) (io.ReadCloser, error) {
	url := b.apiURL(storagePath, true)
	resp, err := b.doRequest(ctx, "GET", url, nil, "")
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, fmt.Errorf("supabase download failed: %d", resp.StatusCode)
	}
	return resp.Body, nil
}

func (b *SupabaseBackend) Delete(ctx context.Context, storagePath string) error {
	url := fmt.Sprintf("%s/storage/v1/object/%s/%s", b.baseURL, b.bucket, storagePath)
	resp, err := b.doRequest(ctx, "DELETE", url, nil, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 204 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("supabase delete failed: %d %s", resp.StatusCode, string(body))
	}
	return nil
}

func (b *SupabaseBackend) Exists(ctx context.Context, storagePath string) (bool, error) {
	url := b.apiURL(storagePath, true)
	resp, err := b.doRequest(ctx, "HEAD", url, nil, "")
	if err != nil {
		return false, err
	}
	resp.Body.Close()
	return resp.StatusCode == 200, nil
}

func (b *SupabaseBackend) GetSize(ctx context.Context, storagePath string) (int64, error) {
	url := b.apiURL(storagePath, true)
	resp, err := b.doRequest(ctx, "HEAD", url, nil, "")
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("file not found: %d", resp.StatusCode)
	}
	return resp.ContentLength, nil
}

func (b *SupabaseBackend) List(ctx context.Context, prefix string) ([]string, error) {
	// Supabase list objects API
	url := fmt.Sprintf("%s/storage/v1/object/list/%s?prefix=%s", b.baseURL, b.bucket, prefix)
	resp, err := b.doRequest(ctx, "POST", url, bytes.NewReader([]byte("{}")), "application/json")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("supabase list failed: %d", resp.StatusCode)
	}
	// Parse JSON response - for simplicity return empty for now
	// Supabase returns array of {name, id, ...}
	return []string{}, nil
}
