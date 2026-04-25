package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_fileMissing_usesDefault(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatal(err)
	}
	if c != Default {
		t.Fatalf("got %#v", c)
	}
}

func TestLoad_partialMergesDefaults(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.json")
	if err := os.WriteFile(p, []byte(`{"listenAddress": ":9999"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.ListenAddress != ":9999" {
		t.Fatalf("listen: %q", c.ListenAddress)
	}
	if c.DataPath != "" || c.MapPath != "" {
		t.Fatalf("defaults: %#v", c)
	}
	if c.SecretsCSV != Default.SecretsCSV || c.SecretsFile != Default.SecretsFile || c.CredentialsFile != Default.CredentialsFile {
		t.Fatalf("defaults not merged: %#v", c)
	}
	if c.ParametersCSV != Default.ParametersCSV || c.ParametersFile != Default.ParametersFile {
		t.Fatalf("parameter defaults: %#v", c)
	}
	if c.S3DataPath != Default.S3DataPath {
		t.Fatalf("s3DataPath default: %#v", c)
	}
	if c.SQSDataPath != Default.SQSDataPath {
		t.Fatalf("sqsDataPath default: %#v", c)
	}
}

func TestLoad_mapPath_preserved(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "s.json")
	if err := os.WriteFile(p, []byte(`{"mapPath": "/data/volume"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.MapPath != "/data/volume" {
		t.Fatalf("mapPath: %q", c.MapPath)
	}
}

func TestResolveWithDataPath(t *testing.T) {
	if got := ResolveWithDataPath("/usr", "secrets.csv"); got != filepath.Join("/usr", "secrets.csv") {
		t.Fatalf("got %q", got)
	}
	if got := ResolveWithDataPath("  /var/lib/  ", "creds/credentials.csv"); got != filepath.Join("/var/lib", "credentials.csv") {
		t.Fatalf("got %q", got)
	}
	if got := ResolveWithDataPath("", "a.csv"); got != JoinDot("a.csv") {
		t.Fatalf("empty dataPath: got %q", got)
	}
}

func TestJoinDot(t *testing.T) {
	if got := JoinDot("foo.json"); got != filepath.Join(".", "foo.json") {
		t.Fatalf("got %q", got)
	}
	if got := JoinDot("/tmp/x"); got != "/tmp/x" {
		t.Fatalf("abs got %q", got)
	}
}
