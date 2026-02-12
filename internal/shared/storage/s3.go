package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/nextconvert/backend/internal/shared/config"
)

// S3Backend implements S3-compatible storage (AWS S3, MinIO, etc.)
type S3Backend struct {
	client *s3.Client
	bucket string
}

// NewS3Backend creates a new S3 storage backend
func NewS3Backend(cfg config.StorageConfig) (*S3Backend, error) {
	if cfg.S3Bucket == "" {
		return nil, fmt.Errorf("S3_BUCKET is required for s3 storage backend")
	}

	var opts []func(*awsconfig.LoadOptions) error

	// Set region
	region := cfg.S3Region
	if region == "" {
		region = "us-east-1"
	}
	opts = append(opts, awsconfig.WithRegion(region))

	// Set credentials if provided (otherwise uses default AWS credential chain)
	if cfg.S3AccessKey != "" && cfg.S3SecretKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.S3AccessKey, cfg.S3SecretKey, ""),
		))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Build S3 client options
	s3Opts := []func(*s3.Options){}

	// Custom endpoint (for MinIO, DigitalOcean Spaces, etc.)
	if cfg.S3Endpoint != "" {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.S3Endpoint)
			o.UsePathStyle = true // Required for most S3-compatible services
		})
	}

	client := s3.NewFromConfig(awsCfg, s3Opts...)

	return &S3Backend{
		client: client,
		bucket: cfg.S3Bucket,
	}, nil
}

// objectKey returns the S3 object key (zone/filename)
func (b *S3Backend) objectKey(zone Zone, filename string) string {
	return path.Join(string(zone), filename)
}

func (b *S3Backend) Store(ctx context.Context, zone Zone, filename string, reader io.Reader) (string, error) {
	key := b.objectKey(zone, filename)

	// Stream to S3 without loading entire file into memory
	var contentLength int64
	var body io.Reader = reader

	if seeker, ok := reader.(io.ReadSeeker); ok {
		// Reader supports seeking (e.g. *os.File, multipart.File) -- get size efficiently
		current, _ := seeker.Seek(0, io.SeekCurrent)
		end, err := seeker.Seek(0, io.SeekEnd)
		if err == nil {
			contentLength = end - current
			seeker.Seek(current, io.SeekStart)
		}
	}

	if contentLength == 0 {
		// Non-seekable reader: buffer to temp file to get a known size
		tmpFile, err := os.CreateTemp("", "s3-upload-*")
		if err != nil {
			return "", fmt.Errorf("failed to create temp file: %w", err)
		}
		defer os.Remove(tmpFile.Name())
		defer tmpFile.Close()

		written, err := io.Copy(tmpFile, reader)
		if err != nil {
			return "", fmt.Errorf("failed to buffer data: %w", err)
		}
		contentLength = written
		tmpFile.Seek(0, io.SeekStart)
		body = tmpFile
	}

	_, err := b.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(b.bucket),
		Key:           aws.String(key),
		Body:          body,
		ContentLength: aws.Int64(contentLength),
	})
	if err != nil {
		return "", fmt.Errorf("s3 upload failed: %w", err)
	}

	return key, nil
}

func (b *S3Backend) Retrieve(ctx context.Context, storagePath string) (io.ReadCloser, error) {
	resp, err := b.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(storagePath),
	})
	if err != nil {
		return nil, fmt.Errorf("s3 download failed: %w", err)
	}
	return resp.Body, nil
}

func (b *S3Backend) Delete(ctx context.Context, storagePath string) error {
	_, err := b.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(storagePath),
	})
	if err != nil {
		return fmt.Errorf("s3 delete failed: %w", err)
	}
	return nil
}

func (b *S3Backend) Exists(ctx context.Context, storagePath string) (bool, error) {
	_, err := b.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(storagePath),
	})
	if err != nil {
		// Check if it's a NotFound error
		var notFound *types.NotFound
		if ok := isNotFoundError(err, notFound); ok {
			return false, nil
		}
		return false, fmt.Errorf("s3 head failed: %w", err)
	}
	return true, nil
}

func (b *S3Backend) GetSize(ctx context.Context, storagePath string) (int64, error) {
	resp, err := b.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(storagePath),
	})
	if err != nil {
		return 0, fmt.Errorf("s3 head failed: %w", err)
	}
	if resp.ContentLength != nil {
		return *resp.ContentLength, nil
	}
	return 0, nil
}

// GetPresignedUploadURL generates a presigned PUT URL for direct client-to-S3 upload
func (b *S3Backend) GetPresignedUploadURL(ctx context.Context, zone Zone, filename string, contentType string, expiry time.Duration) (string, string, error) {
	key := b.objectKey(zone, filename)

	presignClient := s3.NewPresignClient(b.client)

	input := &s3.PutObjectInput{
		Bucket:      aws.String(b.bucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
	}

	resp, err := presignClient.PresignPutObject(ctx, input, s3.WithPresignExpires(expiry))
	if err != nil {
		return "", "", fmt.Errorf("failed to generate presigned URL: %w", err)
	}

	return resp.URL, key, nil
}

func (b *S3Backend) List(ctx context.Context, prefix string) ([]string, error) {
	var keys []string

	paginator := s3.NewListObjectsV2Paginator(b.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(b.bucket),
		Prefix: aws.String(prefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("s3 list failed: %w", err)
		}
		for _, obj := range page.Contents {
			if obj.Key != nil {
				keys = append(keys, *obj.Key)
			}
		}
	}

	return keys, nil
}

// isNotFoundError checks if the error is an S3 not-found error
func isNotFoundError(err error, _ *types.NotFound) bool {
	if err == nil {
		return false
	}
	// Check error message for common not-found indicators
	errStr := err.Error()
	return contains(errStr, "NotFound") || contains(errStr, "NoSuchKey") || contains(errStr, "404")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
