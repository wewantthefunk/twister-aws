package main

import (
	"testing"
)

func TestGetenvFirst_prefersFirstSet(t *testing.T) {
	t.Setenv("TWISTER_A", "")
	t.Setenv("TWISTER_B", "from-b")
	t.Setenv("TWISTER_C", "from-c")

	if got := getenvFirst([]string{"TWISTER_A", "TWISTER_B", "TWISTER_C"}, "fallback"); got != "from-b" {
		t.Fatalf("got %q, want from-b", got)
	}
}

func TestGetenvFirst_fallback(t *testing.T) {
	t.Setenv("TWISTER_ONLY_EMPTY", "")

	if got := getenvFirst([]string{"TWISTER_ONLY_EMPTY", "ALSO_UNSET_XYZ"}, "fallback"); got != "fallback" {
		t.Fatalf("got %q, want fallback", got)
	}
}

func TestGetenvFirst_firstKeyWins(t *testing.T) {
	t.Setenv("P_FIRST", "one")
	t.Setenv("P_SECOND", "two")

	if got := getenvFirst([]string{"P_FIRST", "P_SECOND"}, "x"); got != "one" {
		t.Fatalf("got %q", got)
	}
}
