package s3buckets

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Manager maps S3 buckets to subdirectories of Root.
type Manager struct {
	Root string
}

// NewManager returns a Manager. Root should be a clean path; Root is created on first use.
func NewManager(root string) *Manager {
	return &Manager{Root: filepath.Clean(root)}
}

// BucketPath is the on-disk path for a bucket (may not exist).
func (m *Manager) BucketPath(name string) string {
	return filepath.Join(m.Root, name)
}

// ensureRoot creates Root if needed.
func (m *Manager) ensureRoot() error {
	if m == nil || m.Root == "" {
		return errors.New("s3: empty root")
	}
	return os.MkdirAll(m.Root, 0o750)
}

// CreateBucket creates a new directory for the bucket, or returns ErrBucketAlreadyExists.
func (m *Manager) CreateBucket(name string) error {
	if !IsValidBucketName(name) {
		return ErrInvalidBucketName
	}
	if err := m.ensureRoot(); err != nil {
		return err
	}
	p := m.BucketPath(name)
	if fi, err := os.Stat(p); err == nil {
		if fi.IsDir() {
			return ErrBucketAlreadyExists
		}
		return fmt.Errorf("s3: %s exists and is not a directory", p)
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Mkdir(p, 0o750)
}

// DeleteBucket removes an **empty** bucket directory, or ErrNoSuchBucket / ErrBucketNotEmpty.
func (m *Manager) DeleteBucket(name string) error {
	if !IsValidBucketName(name) {
		return ErrInvalidBucketName
	}
	if m == nil || m.Root == "" {
		return errors.New("s3: empty root")
	}
	p := m.BucketPath(name)
	if fi, err := os.Stat(p); err != nil {
		if os.IsNotExist(err) {
			return ErrNoSuchBucket
		}
		return err
	} else if !fi.IsDir() {
		return fmt.Errorf("s3: %s is not a directory", p)
	}
	if err := os.Remove(p); err != nil {
		if isDirNotEmptyErr(err) {
			return ErrBucketNotEmpty
		}
		return err
	}
	return nil
}
