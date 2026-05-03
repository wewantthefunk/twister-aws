package awsserver

import (
	"encoding/json"
	"net/http"

	"github.com/christian/twister/internal/credentials"
	"github.com/christian/twister/internal/ec2"
	"github.com/christian/twister/internal/paramstore"
	"github.com/christian/twister/internal/secretstore"
)

// Refresher reloads file-backed state (credentials CSV, secrets CSV/JSON, parameters CSV/JSON).
// Add new reload steps here when more disk-backed data is introduced.
type Refresher struct {
	Provider           *credentials.Provider
	Store              *secretstore.Store
	SecretsCSVPath     string
	SecretsJSONPath    string
	ParamStore         *paramstore.Store
	ParametersCSVPath  string
	ParametersJSONPath string
	// EC2, if set, reloads EC2 state.json on /refresh.
	EC2 *ec2.Service
}

// RefreshResponse is returned as JSON on success.
type RefreshResponse struct {
	OK             bool   `json:"ok"`
	AccessKeys     int    `json:"accessKeys"`
	Secrets        int    `json:"secrets"`
	Credentials    string `json:"credentialsPath,omitempty"`
	SecretsCSV     string `json:"secretsCSV,omitempty"`
	SecretsJSON    string `json:"secretsJSON,omitempty"`
	Parameters     int    `json:"parameters"`
	ParametersCSV  string `json:"parametersCSV,omitempty"`
	ParametersJSON string `json:"parametersJSON,omitempty"`
}

// Refresh handles GET or POST /refresh: reload credentials and secret store from disk paths
// resolved at process startup (same as main). Unauthenticated; restrict access in production.
func (rf *Refresher) Refresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if rf == nil || rf.Provider == nil || rf.Store == nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "refresh not configured"})
		return
	}

	if err := rf.Provider.ReloadFromFile(); err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	if err := rf.Store.ReloadFromFiles(rf.SecretsCSVPath, rf.SecretsJSONPath); err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	if rf.ParamStore != nil {
		if err := rf.ParamStore.ReloadFromFiles(rf.ParametersCSVPath, rf.ParametersJSONPath); err != nil {
			WriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": err.Error()})
			return
		}
	}
	if rf.EC2 != nil {
		if err := rf.EC2.ReloadState(); err != nil {
			WriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": err.Error()})
			return
		}
	}

	resp := RefreshResponse{
		OK:             true,
		AccessKeys:     rf.Provider.AccessKeyCount(),
		Secrets:        rf.Store.Count(),
		Parameters:     0,
		Credentials:    rf.Provider.CredentialCSVPath(),
		SecretsCSV:     rf.SecretsCSVPath,
		SecretsJSON:    rf.SecretsJSONPath,
		ParametersCSV:  rf.ParametersCSVPath,
		ParametersJSON: rf.ParametersJSONPath,
	}
	if rf.ParamStore != nil {
		resp.Parameters = rf.ParamStore.Count()
	}
	w.Header().Set("Content-Type", ContentTypeJSON)
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(resp)
}
