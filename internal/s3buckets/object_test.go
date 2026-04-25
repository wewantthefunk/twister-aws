package s3buckets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPutGetDeleteObject(t *testing.T) {
	root := t.TempDir()
	m := NewManager(root)
	const reg = "us-east-1"
	if err := m.CreateBucket(reg, "bkt"); err != nil {
		t.Fatal(err)
	}
	data := []byte("hello, object\n")
	if err := m.PutObject(reg, "bkt", "a/go.mod", data); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, reg, "bkt", "a", "go.mod")
	if b, err := os.ReadFile(p); err != nil || string(b) != string(data) {
		t.Fatalf("file: %v %q", err, b)
	}
	opath, _, err := m.GetObjectFile(reg, "bkt", "a/go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if opath != p {
		t.Fatalf("path %q != %q", opath, p)
	}
	if err := m.DeleteObject(reg, "bkt", "a/go.mod"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("file should be gone")
	}
}

func TestParseS3Path(t *testing.T) {
	b, k, err := parseS3Path("/first-bucket/go.mod")
	if err != nil || b != "first-bucket" || k != "go.mod" {
		t.Fatalf("got %q %q %v", b, k, err)
	}
	b, k, err = parseS3Path("/my-bucket")
	if err != nil || b != "my-bucket" || k != "" {
		t.Fatalf("got %q %q", b, k)
	}
}
