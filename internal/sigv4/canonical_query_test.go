package sigv4

import (
	"net/url"
	"testing"
)

func TestCanonicalQueryV4_sortsAndEncodes(t *testing.T) {
	v := url.Values{}
	v.Set("z", "1")
	v.Set("a", "2")
	if got, want := canonicalQueryV4(v), "a=2&z=1"; got != want {
		t.Fatalf("canonicalQueryV4 = %q, want %q", got, want)
	}
}

func TestCanonicalQueryV4_duplicateKeysSortByValue(t *testing.T) {
	v := url.Values{}
	v.Add("k", "b")
	v.Add("k", "a")
	if got, want := canonicalQueryV4(v), "k=a&k=b"; got != want {
		t.Fatalf("canonicalQueryV4 = %q, want %q", got, want)
	}
}

func TestCanonicalQueryV4_uriEncodes(t *testing.T) {
	v := url.Values{}
	v.Set("p", "a b")
	if got, want := canonicalQueryV4(v), "p=a%20b"; got != want {
		t.Fatalf("canonicalQueryV4 = %q, want %q", got, want)
	}
}

func TestCanonicalQueryV4_empty(t *testing.T) {
	if got := canonicalQueryV4(url.Values{}); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}
