package secretstore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStore_ReloadFromFiles_replaces(t *testing.T) {
	dir := t.TempDir()
	csv := filepath.Join(dir, "s.csv")
	if err := os.WriteFile(csv, []byte("name,secretString\na,1\nb,2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewStore()
	if err := s.ReloadFromFiles(csv, ""); err != nil {
		t.Fatal(err)
	}
	if s.Count() < 2 {
		t.Fatalf("count %d", s.Count())
	}
	// Shrink file: one row — old names from CSV not in file should disappear after reload
	if err := os.WriteFile(csv, []byte("name,secretString\na,3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.ReloadFromFiles(csv, ""); err != nil {
		t.Fatal(err)
	}
	if s.Lookup("b") != nil {
		t.Fatal("b should be gone after reload")
	}
	if s.Lookup("a") == nil || s.Lookup("a").SecretString != "3" {
		t.Fatal()
	}
}

func TestStore_ReloadFromFiles_seed(t *testing.T) {
	dir := t.TempDir()
	csv := filepath.Join(dir, "empty.csv")
	if err := os.WriteFile(csv, []byte("name,secretString\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewStore()
	if err := s.ReloadFromFiles(csv, ""); err != nil {
		t.Fatal(err)
	}
	// no data rows, but SeedDefaults adds demo secrets
	if s.Lookup("my-test-secret") == nil {
		t.Fatal("expected seed after reload")
	}
}
