package awsserver

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/christian/twister/internal/credentials"
	"github.com/christian/twister/internal/paramstore"
	"github.com/christian/twister/internal/secretstore"
)

func TestRefresher_Refresh(t *testing.T) {
	dir := t.TempDir()
	cred := filepath.Join(dir, "c.csv")
	if err := os.WriteFile(cred, []byte("access_key_id,secret_access_key\nk,v\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	csv := filepath.Join(dir, "sec.csv")
	if err := os.WriteFile(csv, []byte("name,secretString\nn,val\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pcsv := filepath.Join(dir, "p.csv")
	if err := os.WriteFile(pcsv, []byte("name,region,value\n/p,us-east-1,ok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	prov, err := credentials.FromFile(cred)
	if err != nil {
		t.Fatal(err)
	}
	data := secretstore.NewStore()
	if err := data.ReloadFromFiles(csv, ""); err != nil {
		t.Fatal(err)
	}
	pst := paramstore.NewStore()
	if err := pst.ReloadFromFiles(pcsv, ""); err != nil {
		t.Fatal(err)
	}
	rf := &Refresher{Provider: prov, Store: data, SecretsCSVPath: csv, SecretsJSONPath: "", ParamStore: pst, ParametersCSVPath: pcsv, ParametersJSONPath: ""}
	rr := httptest.NewRecorder()
	rf.Refresh(rr, httptest.NewRequest(http.MethodPost, "/refresh", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("code %d %s", rr.Code, rr.Body.String())
	}
}

func TestRefresher_Refresh_notConfigured(t *testing.T) {
	var rf *Refresher
	rr := httptest.NewRecorder()
	rf.Refresh(rr, httptest.NewRequest(http.MethodGet, "/refresh", nil))
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("code %d", rr.Code)
	}
}
