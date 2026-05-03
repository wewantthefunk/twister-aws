package ssm

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
	"github.com/christian/twister/internal/paramstore"
)

func testParamCSV(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "parameters.csv")
}

func TestService_GetParameter(t *testing.T) {
	st := paramstore.NewStore()
	st.Put(&paramstore.ParameterRecord{
		Region: "eu-central-1", Name: "/n", Type: "String", Value: "v", Version: 3, LastModified: time.Unix(20, 0).UTC(),
	})
	sv := New(st, testParamCSV(t))
	body := `{"Name":"/n"}`
	rr := httptest.NewRecorder()
	ctx := awsserver.WithSigningRegion(context.Background(), "eu-central-1")
	sv.Handle(rr, httptest.NewRequest(http.MethodPost, "/", nil).WithContext(ctx), "GetParameter", []byte(body))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var out struct {
		Parameter struct {
			Name    string
			Value   string
			Version int
		} `json:"Parameter"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Parameter.Name != "/n" || out.Parameter.Value != "v" || out.Parameter.Version != 3 {
		t.Fatalf("got %+v", out)
	}
}

func TestService_GetParameter_wrongRegion(t *testing.T) {
	st := paramstore.NewStore()
	st.Put(&paramstore.ParameterRecord{Region: "ap-northeast-1", Name: "/only-tokyo", Type: "String", Value: "x", Version: 1, LastModified: time.Unix(1, 0).UTC()})
	sv := New(st, testParamCSV(t))
	ctx := awsserver.WithSigningRegion(context.Background(), "us-east-1")
	rr := httptest.NewRecorder()
	sv.Handle(rr, httptest.NewRequest(http.MethodPost, "/", nil).WithContext(ctx), "GetParameter", []byte(`{"Name":"/only-tokyo"}`))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
}

func TestService_PutParameter_createAndOverwrite(t *testing.T) {
	p := filepath.Join(t.TempDir(), "p.csv")
	st := paramstore.NewStore()
	sv := New(st, p)
	ctx := awsserver.WithSigningRegion(context.Background(), "us-east-1")
	body1 := `{"Name":"/k","Value":"a","Type":"String"}`
	rr1 := httptest.NewRecorder()
	sv.Handle(rr1, httptest.NewRequest(http.MethodPost, "/", nil).WithContext(ctx), "PutParameter", []byte(body1))
	if rr1.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rr1.Code, rr1.Body.String())
	}
	if st.LookupInRegion("/k", "us-east-1") == nil || st.LookupInRegion("/k", "us-east-1").Value != "a" {
		t.Fatal("store after create")
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "/k") {
		t.Fatalf("csv: %s", b)
	}

	body2 := `{"Name":"/k","Value":"b","Type":"String","Overwrite":true}`
	rr2 := httptest.NewRecorder()
	sv.Handle(rr2, httptest.NewRequest(http.MethodPost, "/", nil).WithContext(ctx), "PutParameter", []byte(body2))
	if rr2.Code != http.StatusOK {
		t.Fatalf("update: %d", rr2.Code)
	}
	if st.LookupInRegion("/k", "us-east-1").Value != "b" {
		t.Fatal("v2")
	}
}

func TestService_GetParameter_SecureString_requiresDecryption(t *testing.T) {
	st := paramstore.NewStore()
	st.Put(&paramstore.ParameterRecord{Region: "us-east-1", Name: "/s", Type: "SecureString", Value: "secret", Version: 1, LastModified: time.Unix(1, 0).UTC()})
	sv := New(st, testParamCSV(t))
	ctx := awsserver.WithSigningRegion(context.Background(), "us-east-1")
	rr := httptest.NewRecorder()
	sv.Handle(rr, httptest.NewRequest(http.MethodPost, "/", nil).WithContext(ctx), "GetParameter", []byte(`{"Name":"/s","WithDecryption":false}`))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d", rr.Code)
	}
}

func TestService_unknownOperation(t *testing.T) {
	sv := New(paramstore.NewStore(), testParamCSV(t))
	rr := httptest.NewRecorder()
	sv.Handle(rr, httptest.NewRequest(http.MethodPost, "/", nil), "DescribeParameters", nil)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rr.Code)
	}
}
