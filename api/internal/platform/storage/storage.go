// Package storage: object storage S3-compatible (MinIO / R2 / S3) dengan presigned URL (TAD §5.13).
package storage

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/buildingvision/api/internal/platform/config"
)

type Storage interface {
	PresignPut(ctx context.Context, key, contentType string, size int64, ttl time.Duration) (string, error)
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
	Put(ctx context.Context, key, contentType string, body io.Reader, size int64) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Head(ctx context.Context, key string) (size int64, contentType string, err error)
	Delete(ctx context.Context, key string) error
}

type S3Storage struct {
	client  *s3.Client
	presign *s3.PresignClient
	bucket  string
}

func NewS3(ctx context.Context, cfg config.Config) (*S3Storage, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(cfg.S3Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.S3AccessKey, cfg.S3SecretKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("aws config: %w", err)
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.S3Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.S3Endpoint)
		}
		o.UsePathStyle = cfg.S3UsePathStyle
	})
	// Presigned URL dipakai browser/app (di luar jaringan Docker): tanda tangan dihitung untuk BV_S3_PUBLIC_ENDPOINT
	// (mis. https://app.example.com → Caddy reverse-proxy path /<bucket>/* ke MinIO tanpa mengubah path & Host).
	presignClient := client
	if cfg.S3PublicEndpoint != "" && cfg.S3PublicEndpoint != cfg.S3Endpoint {
		presignClient = s3.NewFromConfig(awsCfg, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.S3PublicEndpoint)
			o.UsePathStyle = cfg.S3UsePathStyle
		})
	}
	return &S3Storage{client: client, presign: s3.NewPresignClient(presignClient), bucket: cfg.S3Bucket}, nil
}

// EnsureBucket membuat bucket bila belum ada (local/MinIO).
func (s *S3Storage) EnsureBucket(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.bucket)})
	if err == nil {
		return nil
	}
	_, err = s.client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(s.bucket)})
	return err
}

func (s *S3Storage) PresignPut(ctx context.Context, key, contentType string, size int64, ttl time.Duration) (string, error) {
	out, err := s.presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		ContentType:   aws.String(contentType),
		ContentLength: aws.Int64(size),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", err
	}
	return out.URL, nil
}

func (s *S3Storage) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	out, err := s.presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", err
	}
	return out.URL, nil
}

func (s *S3Storage) Put(ctx context.Context, key, contentType string, body io.Reader, size int64) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(key), Body: body,
		ContentType: aws.String(contentType), ContentLength: aws.Int64(size),
	})
	return err
}

func (s *S3Storage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return nil, err
	}
	return out.Body, nil
}

func (s *S3Storage) Head(ctx context.Context, key string) (int64, string, error) {
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return 0, "", err
	}
	return aws.ToInt64(out.ContentLength), aws.ToString(out.ContentType), nil
}

func (s *S3Storage) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	return err
}

// ObjectKey: org/{org_id}/{yyyy}/{mm}/{attachment_id}.{ext}
func ObjectKey(orgID, attachmentID string, at time.Time, ext string) string {
	return fmt.Sprintf("org/%s/%04d/%02d/%s%s", orgID, at.Year(), int(at.Month()), attachmentID, ext)
}

// ---------- In-memory (test / local tanpa MinIO) ----------

type MemoryStorage struct {
	objects map[string][]byte
	types   map[string]string
	baseURL string
}

func NewMemory(baseURL string) *MemoryStorage {
	return &MemoryStorage{objects: map[string][]byte{}, types: map[string]string{}, baseURL: baseURL}
}
func (m *MemoryStorage) PresignPut(_ context.Context, key, _ string, _ int64, _ time.Duration) (string, error) {
	return m.baseURL + "/_dev/upload/" + key, nil
}
func (m *MemoryStorage) PresignGet(_ context.Context, key string, _ time.Duration) (string, error) {
	return m.baseURL + "/_dev/download/" + key, nil
}
func (m *MemoryStorage) Put(_ context.Context, key, ct string, body io.Reader, _ int64) error {
	b, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	m.objects[key] = b
	m.types[key] = ct
	return nil
}
func (m *MemoryStorage) Get(_ context.Context, key string) (io.ReadCloser, error) {
	b, ok := m.objects[key]
	if !ok {
		return nil, fmt.Errorf("object %s not found", key)
	}
	return io.NopCloser(bytesReader(b)), nil
}
func (m *MemoryStorage) Head(_ context.Context, key string) (int64, string, error) {
	b, ok := m.objects[key]
	if !ok {
		return 0, "", fmt.Errorf("object %s not found", key)
	}
	return int64(len(b)), m.types[key], nil
}
func (m *MemoryStorage) Delete(_ context.Context, key string) error {
	delete(m.objects, key)
	return nil
}
func (m *MemoryStorage) Exists(key string) bool { _, ok := m.objects[key]; return ok }
