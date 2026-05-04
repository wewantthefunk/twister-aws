package ssm

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/christian/twister/internal/awsserver"
	"github.com/christian/twister/internal/paramstore"
)

// Service implements SSM (Parameter Store) JSON 1.1 for selected operations.
type Service struct {
	Store         *paramstore.Store
	ParametersCSV string
}

// New returns a service backed by the parameter store; ParametersCSV is used to persist PutParameter.
func New(store *paramstore.Store, parametersCSV string) *Service {
	return &Service{Store: store, ParametersCSV: parametersCSV}
}

// ServiceName returns the SigV4 / X-Amz-Target service prefix "ssm".
func (s *Service) ServiceName() string {
	return "ssm"
}

// Handle dispatches ssm.SomeOperation.
func (s *Service) Handle(w http.ResponseWriter, r *http.Request, op string, body []byte) {
	switch op {
	case "GetParameter":
		s.getParameter(w, r, body)
	case "PutParameter":
		s.putParameter(w, r, body)
	default:
		awsserver.WriteJSON(w, http.StatusBadRequest, awsserver.ErrorResponse{
			Type:    "InvalidRequestException",
			Message: "Unknown operation for service ssm: " + op,
		})
	}
}

type getParameterRequest struct {
	Name           string `json:"Name"`
	WithDecryption bool   `json:"WithDecryption"`
}

type getParameterResponse struct {
	Parameter parameterBody `json:"Parameter"`
}

type parameterBody struct {
	ARN              string  `json:"ARN"`
	Name             string  `json:"Name"`
	Type             string  `json:"Type"`
	Value            string  `json:"Value"`
	Version          int     `json:"Version"`
	DataType         string  `json:"DataType"`
	LastModifiedDate float64 `json:"LastModifiedDate"`
}

type putParameterRequest struct {
	Name      string `json:"Name"`
	Value     string `json:"Value"`
	Type      string `json:"Type"`
	Overwrite bool   `json:"Overwrite"`
}

type putParameterResponse struct {
	Version int    `json:"Version"`
	Tier    string `json:"Tier"`
}

func (s *Service) getParameter(w http.ResponseWriter, r *http.Request, body []byte) {
	var req getParameterRequest
	if err := json.Unmarshal(body, &req); err != nil || strings.TrimSpace(req.Name) == "" {
		awsserver.WriteJSON(w, http.StatusBadRequest, awsserver.ErrorResponse{
			Type:    "InvalidRequestException",
			Message: "Name is required for GetParameter",
		})
		return
	}
	region := awsserver.RegionFromContext(r.Context())
	rec := s.Store.LookupInRegion(req.Name, region)
	if rec == nil {
		awsserver.WriteJSON(w, http.StatusBadRequest, awsserver.ErrorResponse{
			Type:    "ParameterNotFound",
			Message: "Parameter " + req.Name + " not found",
		})
		return
	}
	if rec.Type == "SecureString" && !req.WithDecryption {
		awsserver.WriteJSON(w, http.StatusBadRequest, awsserver.ErrorResponse{
			Type:    "InvalidRequestException",
			Message: "WithDecryption must be true when GetParameter is called for a SecureString.",
		})
		return
	}
	dt := "text"
	if rec.Type == "StringList" {
		dt = "text"
	}
	arn := paramstore.SynthesizeParameterARN(region, rec.Name)
	awsserver.WriteJSON(w, http.StatusOK, getParameterResponse{
		Parameter: parameterBody{
			ARN:              arn,
			Name:             rec.Name,
			Type:             rec.Type,
			Value:            rec.Value,
			Version:          rec.Version,
			DataType:         dt,
			LastModifiedDate: paramstore.LastModifiedFloat(rec.LastModified),
		},
	})
}

func (s *Service) putParameter(w http.ResponseWriter, r *http.Request, body []byte) {
	var req putParameterRequest
	if err := json.Unmarshal(body, &req); err != nil {
		awsserver.WriteJSON(w, http.StatusBadRequest, awsserver.ErrorResponse{
			Type:    "InvalidRequestException",
			Message: "invalid JSON body for PutParameter",
		})
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		awsserver.WriteJSON(w, http.StatusBadRequest, awsserver.ErrorResponse{
			Type:    "InvalidRequestException",
			Message: "Name is required for PutParameter",
		})
		return
	}
	region := paramstore.NormalizeRegion(awsserver.RegionFromContext(r.Context()))
	ptype := strings.TrimSpace(req.Type)
	if ptype == "" {
		ptype = "String"
	}
	if ptype != "String" && ptype != "StringList" && ptype != "SecureString" {
		awsserver.WriteJSON(w, http.StatusBadRequest, awsserver.ErrorResponse{
			Type:    "InvalidRequestException",
			Message: "Type must be String, StringList, or SecureString",
		})
		return
	}
	existing := s.Store.LookupInRegion(name, region)
	if existing != nil && !req.Overwrite {
		awsserver.WriteJSON(w, http.StatusBadRequest, awsserver.ErrorResponse{
			Type:    "ParameterAlreadyExists",
			Message: "A parameter with this name is already in use",
		})
		return
	}
	ver := 1
	if existing != nil {
		ver = existing.Version + 1
	}
	now := time.Now().UTC()

	rec := &paramstore.ParameterRecord{
		Region:       region,
		Name:         name,
		Type:         ptype,
		Value:        req.Value,
		Version:      ver,
		LastModified: now,
	}
	if err := s.Store.UpsertPersist(s.ParametersCSV, rec); err != nil {
		awsserver.WriteJSON(w, http.StatusInternalServerError, awsserver.ErrorResponse{
			Type:    "InternalServiceError",
			Message: err.Error(),
		})
		return
	}
	awsserver.WriteJSON(w, http.StatusOK, putParameterResponse{Version: ver, Tier: "Standard"})
}
