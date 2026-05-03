package s3buckets

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const testReg = "us-east-1"

func TestManager_CreateDeleteBucket(t *testing.T) {
	root := t.TempDir()
	m := NewManager(root)
	if err := m.CreateBucket(testReg, "my-test-buck"); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, "us-east-1", "my-test-buck")
	if st, err := os.Stat(p); err != nil || !st.IsDir() {
		t.Fatalf("dir: %v", err)
	}
	if err := m.CreateBucket(testReg, "my-test-buck"); !errors.Is(err, ErrBucketAlreadyExists) {
		t.Fatalf("second create: %v", err)
	}
	if err := m.DeleteBucket(testReg, "my-test-buck"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("expected gone: %v", err)
	}
}

func TestManager_DeleteBucket_notEmpty(t *testing.T) {
	root := t.TempDir()
	m := NewManager(root)
	if err := m.CreateBucket(testReg, "not-empty"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(m.BucketPath(testReg, "not-empty"), "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.DeleteBucket(testReg, "not-empty"); !errors.Is(err, ErrBucketNotEmpty) {
		t.Fatalf("want ErrBucketNotEmpty, got %v", err)
	}
}

func TestManager_RegionIsolation(t *testing.T) {
	root := t.TempDir()
	m := NewManager(root)
	if err := m.CreateBucket("us-west-2", "same-name"); err != nil {
		t.Fatal(err)
	}
	if err := m.CreateBucket("eu-west-1", "same-name"); err != nil {
		t.Fatal(err)
	}
	if err := m.DeleteBucket("us-west-2", "same-name"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(m.BucketPath("us-west-2", "same-name")); !os.IsNotExist(err) {
		t.Fatal("west-2 should be gone")
	}
	if err := m.DeleteBucket("eu-west-1", "same-name"); err != nil {
		t.Fatal(err)
	}
}
