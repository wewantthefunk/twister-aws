package s3buckets

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestManager_CreateDeleteBucket(t *testing.T) {
	root := t.TempDir()
	m := NewManager(root)
	if err := m.CreateBucket("my-test-buck"); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, "my-test-buck")
	if st, err := os.Stat(p); err != nil || !st.IsDir() {
		t.Fatalf("dir: %v", err)
	}
	if err := m.CreateBucket("my-test-buck"); !errors.Is(err, ErrBucketAlreadyExists) {
		t.Fatalf("second create: %v", err)
	}
	if err := m.DeleteBucket("my-test-buck"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("expected gone: %v", err)
	}
}

func TestManager_DeleteBucket_notEmpty(t *testing.T) {
	root := t.TempDir()
	m := NewManager(root)
	if err := m.CreateBucket("not-empty"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(m.BucketPath("not-empty"), "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.DeleteBucket("not-empty"); !errors.Is(err, ErrBucketNotEmpty) {
		t.Fatalf("want ErrBucketNotEmpty, got %v", err)
	}
}
