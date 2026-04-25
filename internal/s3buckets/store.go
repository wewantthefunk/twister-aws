package s3buckets

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Manager maps S3 buckets to subdirectories of Root, one level per region:
//   Root/{region}/{bucket}/
type Manager struct {
	Root string
	// Events, if set, receives S3 event JSON when bucket notifications (ObjectCreated) match.
	Events S3EventSink
}

// NewManager returns a Manager. Root should be a clean path; per-region parents are created on demand.
func NewManager(root string) *Manager {
	return &Manager{Root: filepath.Clean(root)}
}

// BucketPath is the on-disk path for a bucket in a given region (may not exist).
func (m *Manager) BucketPath(region, name string) string {
	r := NormalizeRegion(region)
	return filepath.Join(m.Root, r, name)
}

// ensureRegionRoot creates Root and the region directory.
func (m *Manager) ensureRegionRoot(region string) error {
	if m == nil || m.Root == "" {
		return errors.New("s3: empty root")
	}
	r := NormalizeRegion(region)
	if !IsValidRegionSegment(r) {
		return ErrInvalidRegion
	}
	return os.MkdirAll(filepath.Join(m.Root, r), 0o750)
}

// CreateBucket creates a new directory for the bucket in the signing region, or returns ErrBucketAlreadyExists.
func (m *Manager) CreateBucket(region, name string) error {
	if !IsValidRegionSegment(NormalizeRegion(region)) {
		return ErrInvalidRegion
	}
	if !IsValidBucketName(name) {
		return ErrInvalidBucketName
	}
	if err := m.ensureRegionRoot(region); err != nil {
		return err
	}
	p := m.BucketPath(region, name)
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

// DeleteBucket removes an **empty** bucket directory in the signing region, or ErrNoSuchBucket / ErrBucketNotEmpty.
func (m *Manager) DeleteBucket(region, name string) error {
	if !IsValidRegionSegment(NormalizeRegion(region)) {
		return ErrInvalidRegion
	}
	if !IsValidBucketName(name) {
		return ErrInvalidBucketName
	}
	if m == nil || m.Root == "" {
		return errors.New("s3: empty root")
	}
	p := m.BucketPath(region, name)
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
	np, err := m.notificationFilePath(region, name)
	if err == nil {
		_ = os.Remove(np)
	}
	return nil
}
