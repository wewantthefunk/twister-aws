package s3buckets

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// objectPath returns the on-disk file path for an object, or an error if the key is invalid.
func (m *Manager) objectPath(region, bucket, key string) (string, error) {
	if !IsValidRegionSegment(NormalizeRegion(region)) {
		return "", ErrInvalidRegion
	}
	if !IsValidBucketName(bucket) {
		return "", ErrInvalidBucketName
	}
	if err := validateObjectKey(key); err != nil {
		return "", err
	}
	if m == nil || m.Root == "" {
		return "", errors.New("s3: empty root")
	}
	parts := strings.Split(key, "/")
	path := m.BucketPath(region, bucket)
	for _, p := range parts {
		path = filepath.Join(path, p)
	}
	return path, nil
}

func validateObjectKey(key string) error {
	if key == "" {
		return ErrInvalidObjectKey
	}
	// S3 allows many chars; we reject path tricks and empty segments
	for _, p := range strings.Split(key, "/") {
		if p == "" || p == "." || p == ".." {
			return ErrInvalidObjectKey
		}
	}
	return nil
}

// PutObject writes an object; the bucket directory must already exist.
func (m *Manager) PutObject(region, bucket, key string, data []byte) error {
	p, err := m.objectPath(region, bucket, key)
	if err != nil {
		return err
	}
	if fi, err := os.Stat(m.BucketPath(region, bucket)); err != nil {
		if os.IsNotExist(err) {
			return ErrNoSuchBucket
		}
		return err
	} else if !fi.IsDir() {
		return fmt.Errorf("s3: %s is not a bucket", bucket)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		return err
	}
	sum := md5.Sum(data)
	etag := hex.EncodeToString(sum[:])
	m.fireObjectCreated(region, bucket, key, int64(len(data)), etag)
	return nil
}

// GetObjectFile returns the file path, stat, and an error. Caller opens the file.
func (m *Manager) GetObjectFile(region, bucket, key string) (path string, fi os.FileInfo, err error) {
	if _, err := os.Stat(m.BucketPath(region, bucket)); err != nil {
		if os.IsNotExist(err) {
			return "", nil, ErrNoSuchBucket
		}
		return "", nil, err
	}
	p, err := m.objectPath(region, bucket, key)
	if err != nil {
		return "", nil, err
	}
	if fi, err = os.Stat(p); err != nil {
		if os.IsNotExist(err) {
			return "", nil, ErrNoSuchKey
		}
		return "", nil, err
	}
	if fi.IsDir() {
		return "", nil, ErrInvalidObjectKey
	}
	return p, fi, nil
}

// DeleteObject removes one object file.
func (m *Manager) DeleteObject(region, bucket, key string) error {
	if _, err := os.Stat(m.BucketPath(region, bucket)); err != nil {
		if os.IsNotExist(err) {
			return ErrNoSuchBucket
		}
		return err
	}
	p, err := m.objectPath(region, bucket, key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil {
		if os.IsNotExist(err) {
			return ErrNoSuchKey
		}
		return err
	}
	return nil
}
