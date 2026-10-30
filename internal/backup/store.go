package backup

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/raythurman2386/goforge/internal/config"
)

// Object is one key in the bucket.
type Object struct {
	Key  string
	Size int64
}

// Store is the bucket the runner writes to.
type Store interface {
	// Put streams r to key. The size is unknown, so the upload is multipart.
	Put(ctx context.Context, key string, r io.Reader, contentType string) error
	// List returns every key under prefix.
	List(ctx context.Context, prefix string) ([]Object, error)
	// Delete removes keys.
	Delete(ctx context.Context, keys []string) error
}

// partSize bounds memory per upload (one part is buffered at a time) and
// caps one object at 10,000 parts, about 320 GiB.
const partSize = 32 << 20

// S3Store is a Store backed by an S3-compatible bucket via minio-go.
type S3Store struct {
	client *minio.Client
	bucket string
}

// NewS3Store builds a Store from the backup config. It does not contact the
// bucket.
func NewS3Store(cfg config.BackupConfig) (*S3Store, error) {
	host, secure, err := cfg.EndpointHost()
	if err != nil {
		return nil, err
	}
	lookup := minio.BucketLookupAuto
	if cfg.ForcePathStyle {
		lookup = minio.BucketLookupPath
	}
	client, err := minio.New(host, &minio.Options{
		Creds:        credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure:       secure,
		Region:       cfg.Region,
		BucketLookup: lookup,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create S3 client: %w", err)
	}
	return &S3Store{client: client, bucket: cfg.Bucket}, nil
}

// Put implements Store.
func (s *S3Store) Put(ctx context.Context, key string, r io.Reader, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, r, -1, minio.PutObjectOptions{
		ContentType: contentType,
		PartSize:    partSize,
	})
	if err != nil {
		return fmt.Errorf("upload %s: %w", key, err)
	}
	return nil
}

// List implements Store.
func (s *S3Store) List(ctx context.Context, prefix string) ([]Object, error) {
	var out []Object //nolint:prealloc // the listing size is unknown
	for o := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if o.Err != nil {
			return nil, fmt.Errorf("list %s: %w", prefix, o.Err)
		}
		out = append(out, Object{Key: o.Key, Size: o.Size})
	}
	return out, nil
}

// Probe lists at most one key under prefix. It proves the endpoint, the keys,
// and the bucket work, without reading or writing an object. found reports
// whether the prefix already holds a key.
func (s *S3Store) Probe(ctx context.Context, prefix string) (found bool, err error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel() // stops minio-go's listing goroutine after the first key
	o, ok := <-s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true, MaxKeys: 1})
	switch {
	case !ok:
		// Channel closed with no key: empty prefix, or ctx ended.
		return false, ctx.Err()
	case o.Err != nil:
		return false, fmt.Errorf("list %s: %w", prefix, o.Err)
	default:
		return true, nil
	}
}

// Delete implements Store.
func (s *S3Store) Delete(ctx context.Context, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	ch := make(chan minio.ObjectInfo, len(keys))
	for _, k := range keys {
		ch <- minio.ObjectInfo{Key: k}
	}
	close(ch)
	var errs []error //nolint:prealloc // usually empty
	for e := range s.client.RemoveObjects(ctx, s.bucket, ch, minio.RemoveObjectsOptions{}) {
		errs = append(errs, fmt.Errorf("delete %s: %w", e.ObjectName, e.Err))
	}
	return errors.Join(errs...)
}
