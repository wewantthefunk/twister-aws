package iam

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/christian/twister/internal/credentials"
)

func TestIsBootstrapCreateAccessKey(t *testing.T) {
	form := "application/x-www-form-urlencoded; charset=utf-8"
	t.Run("ok", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("Action=CreateAccessKey&Version=2010-05-08"))
		r.Header.Set("Content-Type", form)
		body := []byte("Action=CreateAccessKey&Version=2010-05-08")
		if !IsBootstrapCreateAccessKey(r, body) {
			t.Fatal("expected true")
		}
	})
	t.Run("wrong action", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.Header.Set("Content-Type", form)
		if IsBootstrapCreateAccessKey(r, []byte("Action=ListUsers")) {
			t.Fatal("expected false")
		}
	})
	t.Run("not form CT", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.Header.Set("Content-Type", "application/json")
		if IsBootstrapCreateAccessKey(r, []byte("Action=CreateAccessKey")) {
			t.Fatal("expected false")
		}
	})
	t.Run("nil request", func(t *testing.T) {
		if IsBootstrapCreateAccessKey(nil, []byte("Action=CreateAccessKey")) {
			t.Fatal("expected false")
		}
	})
}

func TestHandle_notConfigured(t *testing.T) {
	rr := httptest.NewRecorder()
	var svc *Service
	svc.Handle(rr, httptest.NewRequest(http.MethodPost, "/", nil), []byte("Action=CreateAccessKey"), "rid")
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("code %d", rr.Code)
	}
}

func TestHandle_wrongContentType(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.csv")
	prov, err := credentials.FromFile(p)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(prov)
	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("Action=CreateAccessKey"))
	r.Header.Set("Content-Type", "application/json")
	svc.Handle(rr, r, []byte("Action=CreateAccessKey"), "rid-1")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("code %d", rr.Code)
	}
}

func TestHandle_CreateAccessKey_persists(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "creds.csv")
	prov, err := credentials.FromFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !prov.IsEmpty() {
		t.Fatal("expected empty bootstrap")
	}
	svc := New(prov)
	body := []byte("Action=CreateAccessKey&Version=2010-05-08")
	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	svc.Handle(rr, r, body, "test-req-id")

	if rr.Code != http.StatusOK {
		t.Fatalf("code %d body %s", rr.Code, rr.Body.String())
	}
	if !strings.HasPrefix(rr.Header().Get("Content-Type"), "text/xml") {
		t.Fatalf("content-type: %q", rr.Header().Get("Content-Type"))
	}
	if prov.AccessKeyCount() != 1 {
		t.Fatalf("keys %d", prov.AccessKeyCount())
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "access_key_id") {
		t.Fatalf("csv: %s", b)
	}

	// response XML contains the new access key id
	var out struct {
		Inner struct {
			AK struct {
				ID string `xml:"AccessKeyId"`
			} `xml:"AccessKey"`
		} `xml:"CreateAccessKeyResult"`
	}
	if err := xml.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("xml: %v", err)
	}
	if out.Inner.AK.ID == "" {
		t.Fatal("empty AccessKeyId in response")
	}
	if !strings.HasPrefix(out.Inner.AK.ID, "AKIA") {
		t.Fatalf("AccessKeyId %q", out.Inner.AK.ID)
	}
}

func TestHandle_UnsupportedAction(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.csv")
	prov, err := credentials.FromFile(p)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(prov)
	body := []byte("Action=ListUsers&Version=2010-05-08")
	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	svc.Handle(rr, r, body, "rid")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("code %d", rr.Code)
	}
}

func TestNewKeyPair_format(t *testing.T) {
	ak, sk, err := newKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	if len(ak) != 20 || !strings.HasPrefix(ak, "AKIA") {
		t.Fatalf("access key: %q", ak)
	}
	if len(sk) != 40 {
		t.Fatalf("secret len %d", len(sk))
	}
}

func TestNewKeyPair_distinct(t *testing.T) {
	a, _, _ := newKeyPair()
	b, _, _ := newKeyPair()
	if a == b {
		t.Fatal("expected different access keys (collision astronomically rare)")
	}
}
