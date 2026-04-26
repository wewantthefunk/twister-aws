package lambda

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegistry_PutGetDeleteList(t *testing.T) {
	dir := t.TempDir()
	r := NewRegistry(dir)
	cfg := &FunctionConfig{
		FunctionName: "my-fn",
		Region:       "us-east-1",
		ImageURI:     "alpine:3.20",
		Handler:      "index.handler",
		Timeout:      10,
		MemorySize:   256,
		PackageType:  "Image",
	}
	if err := r.Put(cfg); err != nil {
		t.Fatal(err)
	}
	got, err := r.Get("my-fn")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.ImageURI != "alpine:3.20" {
		t.Fatalf("Get: %+v", got)
	}
	names, err := r.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "my-fn" {
		t.Fatalf("List: %v", names)
	}
	if err := r.Delete("my-fn"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "functions", "my-fn.json")); !os.IsNotExist(err) {
		t.Fatalf("expected file removed: %v", err)
	}
}

func TestRegistry_invalidName(t *testing.T) {
	r := NewRegistry(t.TempDir())
	if err := r.Put(&FunctionConfig{FunctionName: "bad name", ImageURI: "x"}); err == nil {
		t.Fatal("expected error")
	}
}
