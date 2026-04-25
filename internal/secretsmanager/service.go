package secretsmanager

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/christian/twister/internal/awsserver"
	"github.com/christian/twister/internal/secretstore"
)

// Service implements AWS Secrets Manager JSON API operations for this emulator.
type Service struct {
	Store      *secretstore.Store
	SecretsCSV string // path to secrets.csv; used to persist creates/updates
}

// New returns a Service backed by the given secret store and CSV path for persistence.
func New(store *secretstore.Store, secretsCSVPath string) *Service {
	return &Service{Store: store, SecretsCSV: secretsCSVPath}
}

// ServiceName returns "secretsmanager".
func (s *Service) ServiceName() string {
	return "secretsmanager"
}

// Handle dispatches on the operation name (e.g. GetSecretValue, CreateSecret).
func (s *Service) Handle(w http.ResponseWriter, r *http.Request, op string, body []byte) {
	switch op {
	case "GetSecretValue":
		s.getSecretValue(w, r, body)
	case "CreateSecret":
		s.createSecret(w, r, body)
	default:
		awsserver.WriteJSON(w, http.StatusBadRequest, awsserver.ErrorResponse{
			Type:    "InvalidParameterException",
			Message: "Unknown operation for service secretsmanager: " + op,
		})
	}
}

type getSecretValueRequest struct {
	SecretId     string `json:"SecretId"`
	VersionId    string `json:"VersionId,omitempty"`
	VersionStage string `json:"VersionStage,omitempty"`
}

type getSecretValueResponse struct {
	ARN           string   `json:"ARN"`
	Name          string   `json:"Name"`
	SecretString  string   `json:"SecretString"`
	CreatedDate   float64  `json:"CreatedDate"`
	VersionId     string   `json:"VersionId"`
	VersionStages []string `json:"VersionStages"`
}

type createSecretRequest struct {
	Name           string `json:"Name"`
	SecretString   string `json:"SecretString,omitempty"`
	SecretBinary   string `json:"SecretBinary,omitempty"`
	Description    string `json:"Description,omitempty"`
	ClientRequestToken string `json:"ClientRequestToken,omitempty"`
}

type createSecretResponse struct {
	ARN       string `json:"ARN"`
	Name      string `json:"Name"`
	VersionId string `json:"VersionId"`
}

func (s *Service) getSecretValue(w http.ResponseWriter, r *http.Request, body []byte) {
	var req getSecretValueRequest
	if err := json.Unmarshal(body, &req); err != nil || strings.TrimSpace(req.SecretId) == "" {
		awsserver.WriteJSON(w, http.StatusBadRequest, awsserver.ErrorResponse{
			Type:    "InvalidParameterException",
			Message: "SecretId is required",
		})
		return
	}

	region := awsserver.RegionFromContext(r.Context())
	rec := s.Store.LookupInRegion(req.SecretId, region)
	if rec == nil {
		awsserver.WriteJSON(w, http.StatusBadRequest, awsserver.ErrorResponse{
			Type:    "ResourceNotFoundException",
			Message: "Secrets Manager can't find the specified secret.",
		})
		return
	}

	resp := getSecretValueResponse{
		ARN:           secretstore.SynthesizeARN(region, rec.Name),
		Name:          rec.Name,
		SecretString:  rec.SecretString,
		CreatedDate:   secretstore.CreatedDateFloat(rec.CreatedDate),
		VersionId:     rec.VersionID,
		VersionStages: []string{"AWSCURRENT"},
	}
	awsserver.WriteJSON(w, http.StatusOK, resp)
}

func (s *Service) createSecret(w http.ResponseWriter, r *http.Request, body []byte) {
	var req createSecretRequest
	if err := json.Unmarshal(body, &req); err != nil {
		awsserver.WriteJSON(w, http.StatusBadRequest, awsserver.ErrorResponse{
			Type:    "InvalidParameterException",
			Message: "invalid JSON body",
		})
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		awsserver.WriteJSON(w, http.StatusBadRequest, awsserver.ErrorResponse{
			Type:    "InvalidParameterException",
			Message: "Name is required",
		})
		return
	}

	secretStr := req.SecretString
	if strings.TrimSpace(secretStr) == "" && strings.TrimSpace(req.SecretBinary) != "" {
		awsserver.WriteJSON(w, http.StatusBadRequest, awsserver.ErrorResponse{
			Type:    "InvalidParameterException",
			Message: "SecretString is required for this endpoint (SecretBinary not supported)",
		})
		return
	}
	if strings.TrimSpace(secretStr) == "" {
		awsserver.WriteJSON(w, http.StatusBadRequest, awsserver.ErrorResponse{
			Type:    "InvalidParameterException",
			Message: "You must provide SecretString.",
		})
		return
	}

	now := time.Now().UTC()
	vid := secretstore.NewRandomVersionID()
	region := secretstore.NormalizeRegion(awsserver.RegionFromContext(r.Context()))

	rec := &secretstore.SecretRecord{
		Region:       region,
		Name:         name,
		SecretString: secretStr,
		CreatedDate:  now,
		VersionID:    vid,
	}

	if err := s.Store.UpsertPersist(s.SecretsCSV, rec); err != nil {
		awsserver.WriteJSON(w, http.StatusInternalServerError, awsserver.ErrorResponse{
			Type:    "InternalServiceError",
			Message: err.Error(),
		})
		return
	}

	awsserver.WriteJSON(w, http.StatusOK, createSecretResponse{
		ARN:       secretstore.SynthesizeARN(region, name),
		Name:      name,
		VersionId: vid,
	})
}
