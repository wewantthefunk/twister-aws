package credentials

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProvider_ReloadFromFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.csv")
	if err := os.WriteFile(p, []byte("access_key_id,secret_access_key\na,1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	prov, err := FromFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if prov.Allowlist()["a"] != "1" {
		t.Fatal()
	}
	if err := os.WriteFile(p, []byte("access_key_id,secret_access_key\nb,2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := prov.ReloadFromFile(); err != nil {
		t.Fatal(err)
	}
	if prov.Allowlist()["a"] != "" || prov.Allowlist()["b"] != "2" {
		t.Fatalf("%#v", prov.Allowlist())
	}
}

func TestProvider_ReloadFromFile_missingClears(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.csv")
	if err := os.WriteFile(p, []byte("access_key_id,secret_access_key\nk,v\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	prov, err := FromFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := prov.ReloadFromFile(); err != nil {
		t.Fatal(err)
	}
	if !prov.IsEmpty() {
		t.Fatal("expected empty after file removed")
	}
}
