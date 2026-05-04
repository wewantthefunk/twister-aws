package secretstore

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseNameFromSecretID(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"my-test-secret", "my-test-secret"},
		{"arn:aws:secretsmanager:us-east-1:123:secret:my-test-secret-deadbeef", "my-test-secret"},
		{"some-name-a1b2c3d4", "some-name"},
	}
	for _, tc := range tests {
		if got := ParseNameFromSecretID(tc.in); got != tc.want {
			t.Fatalf("ParseNameFromSecretID(%q) = %q want %q", tc.in, got, tc.want)
		}
	}
}

func TestStore_Lookup(t *testing.T) {
	s := NewStore()
	s.Put(&SecretRecord{Name: "n1", SecretString: "v"})
	if s.Lookup("n1") == nil || s.Lookup("n1").SecretString != "v" {
		t.Fatal("lookup by name")
	}
	if s.Lookup("arn:aws:secretsmanager:r:0:secret:n1-abcdef12") == nil {
		t.Fatal("lookup by arn")
	}
	if s.Lookup("missing") != nil {
		t.Fatal("missing should be nil")
	}
}

func TestSeedDefaults_noSpuriousDefaultRegionWhenNameOnlyInOtherRegion(t *testing.T) {
	s := NewStore()
	s.Put(&SecretRecord{
		Region:       "us-west-1",
		Name:         "other-secret",
		SecretString: "from-csv",
		CreatedDate:  time.Unix(1, 0).UTC(),
		VersionID:    "v1",
	})
	SeedDefaults(s)
	if s.LookupInRegion("other-secret", "us-east-1") != nil {
		t.Fatal("expected no us-east-1 other-secret when name exists only in us-west-1")
	}
	if s.LookupInRegion("other-secret", "us-west-1") == nil {
		t.Fatal("expected us-west-1 other-secret from Put")
	}
}

func TestLoadSecretsJSON(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "secrets.json")
	raw := `[{"name":"x","secretString":"y","createdDate":"2020-01-01T00:00:00Z"}]`
	if err := os.WriteFile(p, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewStore()
	if err := LoadSecretsJSON(p, s); err != nil {
		t.Fatal(err)
	}
	rec := s.Lookup("x")
	if rec == nil || rec.SecretString != "y" {
		t.Fatalf("got %#v", rec)
	}
}
