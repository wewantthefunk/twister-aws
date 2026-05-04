package secretstore

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadSecretsCSV_header(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "s.csv")
	content := "name,secretString\n" + "a,va\n" + "b,vb\n"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewStore()
	if err := LoadSecretsCSV(p, s); err != nil {
		t.Fatal(err)
	}
	if s.Count() != 2 || s.Lookup("a").SecretString != "va" {
		t.Fatalf("got %#v", s.Lookup("a"))
	}
}

func TestLoadSecretsCSV_noHeaderTwoCols(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "s.csv")
	if err := os.WriteFile(p, []byte("x,y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewStore()
	if err := LoadSecretsCSV(p, s); err != nil {
		t.Fatal(err)
	}
	if s.Lookup("x").SecretString != "y" {
		t.Fatal(s.Lookup("x"))
	}
}

func TestLoadSecretsCSV_lastWins(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "s.csv")
	if err := os.WriteFile(p, []byte("name,secretString\nk,1\nk,2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewStore()
	if err := LoadSecretsCSV(p, s); err != nil {
		t.Fatal(err)
	}
	if s.Lookup("k").SecretString != "2" {
		t.Fatalf("got %q", s.Lookup("k").SecretString)
	}
}

func TestLoadSecretsCSV_rfc3339(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "s.csv")
	if err := os.WriteFile(p, []byte("name,secretString,createdDate\nn,v,2019-06-15T10:00:00Z\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewStore()
	if err := LoadSecretsCSV(p, s); err != nil {
		t.Fatal(err)
	}
	want := time.Date(2019, 6, 15, 10, 0, 0, 0, time.UTC)
	if !s.Lookup("n").CreatedDate.Equal(want) {
		t.Fatalf("got %v", s.Lookup("n").CreatedDate)
	}
}

func TestLoadSecretsCSV_twoRegionsSameName(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "s.csv")
	content := "name,region,secretString\n" +
		"s,us-east-1,ea\n" +
		"s,us-west-2,ww\n"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewStore()
	if err := LoadSecretsCSV(p, s); err != nil {
		t.Fatal(err)
	}
	if s.Count() != 2 {
		t.Fatalf("count %d", s.Count())
	}
	if s.LookupInRegion("s", "us-east-1").SecretString != "ea" {
		t.Fatal("east")
	}
	if s.LookupInRegion("s", "us-west-2").SecretString != "ww" {
		t.Fatal("west")
	}
	if s.LookupInRegion("s", "eu-central-1") != nil {
		t.Fatal("expected nil for other region")
	}
}
