package secretsmanager

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/christian/twister/internal/awsserver"
	"github.com/christian/twister/internal/secretstore"
)

func testCSV(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "secrets.csv")
}

func TestService_GetSecretValue(t *testing.T) {
	store := secretstore.NewStore()
	store.Put(&secretstore.SecretRecord{
		Region:       "us-west-2",
		Name:         "a",
		SecretString: "payload",
		VersionID:    "VER",
		CreatedDate:  time.Unix(10, 0).UTC(),
	})
	s := New(store, testCSV(t))
	body := `{"SecretId":"a"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	ctx := awsserver.WithSigningRegion(req.Context(), "us-west-2")
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	s.Handle(rr, req, "GetSecretValue", []byte(body))

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["Name"] != "a" || got["SecretString"] != "payload" {
		t.Fatalf("response %#v", got)
	}
}

func TestService_unknownOperation(t *testing.T) {
	s := New(secretstore.NewStore(), testCSV(t))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`))
	s.Handle(rr, req, "Other", []byte(`{}`))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rr.Code)
	}
}

// Ensure region flows from context (integration with awsserver).
func TestService_regionFromContext(t *testing.T) {
	store := secretstore.NewStore()
	store.Put(&secretstore.SecretRecord{Region: "eu-west-1", Name: "n", SecretString: "x", VersionID: "v", CreatedDate: time.Unix(1, 0).UTC()})
	s := New(store, testCSV(t))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	ctx := awsserver.WithSigningRegion(context.Background(), "eu-west-1")
	s.Handle(rr, req.WithContext(ctx), "GetSecretValue", []byte(`{"SecretId":"n"}`))
	var got map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	arn, _ := got["ARN"].(string)
	if arn == "" || !strings.Contains(arn, "eu-west-1") {
		t.Fatalf("arn = %q", arn)
	}
}

func TestService_CreateSecret_upsert(t *testing.T) {
	p := filepath.Join(t.TempDir(), "secrets.csv")
	st := secretstore.NewStore()
	sv := New(st, p)
	body := `{"Name":"new-one","SecretString":"v1"}`
	rr := httptest.NewRecorder()
	ctx := awsserver.WithSigningRegion(context.Background(), "us-east-1")
	sv.Handle(rr, httptest.NewRequest(http.MethodPost, "/", nil).WithContext(ctx), "CreateSecret", []byte(body))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	if st.Lookup("new-one") == nil || st.Lookup("new-one").SecretString != "v1" {
		t.Fatal("store")
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "new-one") || !strings.Contains(string(b), "v1") {
		t.Fatalf("csv: %s", b)
	}

	rr2 := httptest.NewRecorder()
	body2 := `{"Name":"new-one","SecretString":"v2"}`
	sv.Handle(rr2, httptest.NewRequest(http.MethodPost, "/", nil).WithContext(ctx), "CreateSecret", []byte(body2))
	if rr2.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rr2.Code, rr2.Body.String())
	}
	if st.Lookup("new-one").SecretString != "v2" {
		t.Fatalf("expected v2, got %q", st.Lookup("new-one").SecretString)
	}
	b2, _ := os.ReadFile(p)
	if !strings.Contains(string(b2), "v2") {
		t.Fatalf("csv not updated: %s", b2)
	}
}

func TestService_GetSecretValue_wrongRegion(t *testing.T) {
	store := secretstore.NewStore()
	store.Put(&secretstore.SecretRecord{
		Region:       "us-east-1",
		Name:         "only-in-east",
		SecretString: "x",
		VersionID:    "V",
		CreatedDate:  time.Unix(1, 0).UTC(),
	})
	s := New(store, testCSV(t))
	body := `{"SecretId":"only-in-east"}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	ctx := awsserver.WithSigningRegion(req.Context(), "ap-south-1")
	s.Handle(rr, req.WithContext(ctx), "GetSecretValue", []byte(body))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
}
