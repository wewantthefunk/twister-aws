package paramstore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadParametersCSV_header(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "p.csv")
	content := "name,region,value\n" + "/p,us-east-1,v\n"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewStore()
	if err := LoadParametersCSV(p, s); err != nil {
		t.Fatal(err)
	}
	if s.Count() != 1 || s.LookupInRegion("/p", "us-east-1").Value != "v" {
		t.Fatal()
	}
}

func TestLoadParametersCSV_unheadedTwoCol(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "p.csv")
	if err := os.WriteFile(p, []byte("/x,hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewStore()
	if err := LoadParametersCSV(p, s); err != nil {
		t.Fatal(err)
	}
	if s.LookupInRegion("/x", "us-east-1").Value != "hello" {
		t.Fatal()
	}
}
