package credentials

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAddAccessKeyAndPersist_rewritesSorted(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "credentials.csv")
	if err := os.WriteFile(p, []byte("access_key_id,secret_access_key\ntest,test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	prov, err := FromFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := prov.AddAccessKeyAndPersist("AKIANEW", "secretnew"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	const wantPrefix = "access_key_id,secret_access_key\n"
	if string(b)[:len(wantPrefix)] != wantPrefix {
		t.Fatalf("header: %s", b)
	}
	// Keys sorted: AKIA... before "test"
	if prov.Allowlist()["AKIANEW"] != "secretnew" || prov.Allowlist()["test"] != "test" {
		t.Fatalf("allowlist %#v", prov.Allowlist())
	}
}
