package credentials

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCSV_headerAndRows(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "creds.csv")
	content := "access_key_id,secret_access_key\nak1,sk1\nak2, sk2 \n"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	prov, err := FromFile(p)
	if err != nil {
		t.Fatal(err)
	}
	m := prov.Allowlist()
	if m["ak1"] != "sk1" || m["ak2"] != "sk2" {
		t.Fatalf("map = %#v", m)
	}
}

func TestLoadCSV_noHeader(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "creds.csv")
	if err := os.WriteFile(p, []byte("foo,bar\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prov, err := FromFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if prov.Allowlist()["foo"] != "bar" {
		t.Fatalf("map = %#v", prov.Allowlist())
	}
}

func TestLoadCSV_lastRowWins(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "creds.csv")
	content := "access_key_id,secret_access_key\nsame,first\nsame,second\n"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	prov, err := FromFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if prov.Allowlist()["same"] != "second" {
		t.Fatalf("got %q", prov.Allowlist()["same"])
	}
}

func TestLoadCSV_emptyFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "empty.csv")
	if err := os.WriteFile(p, []byte("access_key_id,secret_access_key\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := LoadCSV(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 0 {
		t.Fatalf("expected empty map, got %d entries", len(m))
	}
}

func TestFromFile_missingFile_bindsPath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "absent.csv")
	prov, err := FromFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !prov.IsEmpty() {
		t.Fatal("expected empty provider")
	}
	if err := prov.AddAccessKeyAndPersist("AKIATEST", "secret"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}
