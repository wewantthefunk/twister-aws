package awsserver

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/christian/twister/internal/credentials"
	"github.com/christian/twister/internal/iam"
)

type stubSvc struct{ name string }

func (s *stubSvc) ServiceName() string { return s.name }
func (s *stubSvc) Handle(http.ResponseWriter, *http.Request, string, []byte) {}

type stubSecrets struct{}

func (s *stubSecrets) ServiceName() string { return "secretsmanager" }
func (s *stubSecrets) Handle(http.ResponseWriter, *http.Request, string, []byte) {
}

func TestNewRouter_duplicateService(t *testing.T) {
	p := credentials.NewProvider(map[string]string{"a": "b"})
	_, err := NewRouter(p, nil, &stubSvc{name: "s"}, &stubSvc{name: "s"})
	if err == nil {
		t.Fatal("expected duplicate service error")
	}
}

func TestNewRouter_nilService(t *testing.T) {
	p := credentials.NewProvider(map[string]string{"a": "b"})
	_, err := NewRouter(p, nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRouter_bootstrapCreateAccessKey_writesCSV(t *testing.T) {
	dir := t.TempDir()
	credPath := filepath.Join(dir, "credentials.csv")
	prov, err := credentials.FromFile(credPath)
	if err != nil {
		t.Fatal(err)
	}
	if !prov.IsEmpty() {
		t.Fatal("expected empty")
	}
	rt, err := NewRouter(prov, iam.New(prov), &stubSecrets{})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	body := "Action=CreateAccessKey&Version=2010-05-08"
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	rt.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/xml") {
		t.Fatalf("content-type: %q", w.Header().Get("Content-Type"))
	}
	if prov.IsEmpty() {
		t.Fatal("expected key in memory")
	}
	if prov.AccessKeyCount() != 1 {
		t.Fatalf("access keys = %d", prov.AccessKeyCount())
	}
	if _, err := os.Stat(credPath); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}
