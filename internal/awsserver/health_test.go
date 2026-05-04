package awsserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealth_get(t *testing.T) {
	rr := httptest.NewRecorder()
	Health(rr, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("code %d", rr.Code)
	}
	if body := rr.Body.String(); body != "ok\n" {
		t.Fatalf("body %q", body)
	}
}

func TestHealth_head(t *testing.T) {
	rr := httptest.NewRecorder()
	Health(rr, httptest.NewRequest(http.MethodHead, "/health", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("code %d", rr.Code)
	}
	if rr.Body.Len() != 0 {
		t.Fatalf("head should not write body, got %q", rr.Body.String())
	}
}

func TestHealth_post(t *testing.T) {
	rr := httptest.NewRecorder()
	Health(rr, httptest.NewRequest(http.MethodPost, "/health", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("code %d", rr.Code)
	}
}
