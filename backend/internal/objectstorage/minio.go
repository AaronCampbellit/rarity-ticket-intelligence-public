package objectstorage

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/attachments"
)

type Config struct {
	Endpoint  string
	Bucket    string
	Region    string
	AccessKey string
	SecretKey string
}

type MinIOStore struct {
	client *minio.Client
	bucket string
}

var _ attachments.ObjectStore = (*MinIOStore)(nil)

func NewMinIOStore(config Config) (*MinIOStore, error) {
	endpoint, err := url.Parse(strings.TrimSpace(config.Endpoint))
	if err != nil || endpoint.Host == "" ||
		(endpoint.Scheme != "http" && endpoint.Scheme != "https") ||
		endpoint.Path != "" && endpoint.Path != "/" ||
		strings.TrimSpace(config.Bucket) == "" ||
		strings.TrimSpace(config.Region) == "" ||
		strings.TrimSpace(config.AccessKey) == "" ||
		strings.TrimSpace(config.SecretKey) == "" {
		return nil, fmt.Errorf("invalid object-storage configuration")
	}
	client, err := minio.New(endpoint.Host, &minio.Options{
		Creds: credentials.NewStaticV4(
			strings.TrimSpace(config.AccessKey),
			strings.TrimSpace(config.SecretKey),
			"",
		),
		Secure: endpoint.Scheme == "https",
		Region: strings.TrimSpace(config.Region),
	})
	if err != nil {
		return nil, fmt.Errorf("configure object storage: %w", err)
	}
	return &MinIOStore{
		client: client, bucket: strings.TrimSpace(config.Bucket),
	}, nil
}

func (s *MinIOStore) Put(
	ctx context.Context,
	key string,
	body io.Reader,
	size int64,
	contentType string,
) error {
	_, err := s.client.PutObject(
		ctx, s.bucket, key, body, size,
		minio.PutObjectOptions{
			ContentType: contentType,
		},
	)
	return err
}

func (s *MinIOStore) Delete(ctx context.Context, key string) error {
	return s.client.RemoveObject(
		ctx, s.bucket, key, minio.RemoveObjectOptions{},
	)
}

func (s *MinIOStore) EnsureBucket(ctx context.Context, create bool, region string) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if !create {
		return fmt.Errorf("object-storage bucket is unavailable")
	}
	return s.client.MakeBucket(
		ctx, s.bucket, minio.MakeBucketOptions{Region: region},
	)
}
