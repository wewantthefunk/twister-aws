package awsserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/christian/twister/internal/credentials"
	"github.com/christian/twister/internal/ec2"
	"github.com/christian/twister/internal/iam"
	"github.com/christian/twister/internal/sqs"
)

// Service is one AWS “service” namespace (e.g. secretsmanager, ssm) handling operations named in X-Amz-Target: service.Operation.
// Implementations should preserve AWS JSON 1.1 request/response contracts for the operations they support.
type Service interface {
	// ServiceName is the canonical prefix used for Router registration (e.g. "secretsmanager", "ssm").
	// The CLI may send a different X-Amz-Target prefix (e.g. AmazonSSM for SSM); the router normalizes that to this name.
	ServiceName() string
	// Handle is called after successful SigV4 verification and content-type checks. op is the part after the first dot
	// in X-Amz-Target (e.g. "GetSecretValue"). body is the full POST body (same bytes the client signed).
	Handle(w http.ResponseWriter, r *http.Request, op string, body []byte)
}

// Router is the main HTTP entry: reads the body, verifies credentials, then routes to IAM (Query) or JSON 1.1 services (X-Amz-Target).
type Router struct {
	Provider *credentials.Provider
	IAM      *iam.Service
	// SQS is the SQS Query API (form POST) when the credential scope service is "sqs". If nil, requests are rejected.
	SQS *sqs.Service
	// EC2 is the EC2 Query API (form POST) when the credential scope service is "ec2". If nil, requests are rejected.
	EC2 *ec2.Service
	services map[string]Service
}

// NewRouter returns a router that verifies every request with provider (SigV4 against the allowlist) and dispatches to services.
// iamService handles IAM when the request’s credential scope service is "iam" (e.g. aws iam create-access-key). Pass nil to reject IAM.
func NewRouter(provider *credentials.Provider, iamService *iam.Service, services ...Service) (*Router, error) {
	r := &Router{
		Provider: provider,
		IAM:      iamService,
		services: make(map[string]Service),
	}
	for _, s := range services {
		if s == nil {
			return nil, fmt.Errorf("awsserver: nil service")
		}
		n := s.ServiceName()
		if n == "" {
			return nil, fmt.Errorf("awsserver: empty ServiceName")
		}
		if _, dup := r.services[n]; dup {
			return nil, fmt.Errorf("awsserver: duplicate service %q", n)
		}
		r.services[n] = s
	}
	return r, nil
}

// canonicalJSONServiceName maps the SigV4 credential scope service and the
// X-Amz-Target prefix to the name used in Router registration. Real clients use
// "ssm" in the scope but "AmazonSSM" in X-Amz-Target for Parameter Store.
func canonicalJSONServiceName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "amazonssm", "ssm":
		return "ssm"
	case "secretsmanager":
		return "secretsmanager"
	case "lambda_20150331":
		return "lambda"
	default:
		return s
	}
}

// ServeHTTP implements the AWS “single endpoint” pattern: POST / with SigV4; JSON 1.1 services use X-Amz-Target, IAM/EC2/SQS use the Query API (form body).
func (rt *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	if err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	// No allowlisted keys: SigV4 cannot be validated (the server has no shared secret to recompute
	// the signature). Allow a single unauthenticated CreateAccessKey so the CLI can create the
	// first key; subsequent calls require a matching key in credentials.csv.
	if rt.Provider != nil && rt.Provider.IsEmpty() && rt.IAM != nil && iam.IsBootstrapCreateAccessKey(r, body) {
		rid := NewRequestID()
		w.Header().Set("x-amzn-RequestId", rid)
		ctx := r.Context()
		ctx = WithSigningRegion(ctx, "us-east-1")
		rt.IAM.Handle(w, r.WithContext(ctx), body, rid)
		return
	}

	region, signingService, err := rt.Provider.VerifyRequest(r, body, time.Now().UTC())
	if err != nil {
		w.Header().Set("Content-Type", ContentTypeJSON)
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(ErrorResponse{Type: "SignatureDoesNotMatch", Message: err.Error()})
		return
	}

	rid := NewRequestID()
	w.Header().Set("x-amzn-RequestId", rid)

	if signingService == "iam" {
		if rt.IAM == nil {
			http.Error(w, "iam service not configured", http.StatusServiceUnavailable)
			return
		}
		ctx := r.Context()
		ctx = WithSigningRegion(ctx, region)
		rt.IAM.Handle(w, r.WithContext(ctx), body, rid)
		return
	}

	if signingService == "sqs" {
		if rt.SQS == nil {
			sqs.WriteNotConfigured(w, rid)
			return
		}
		rt.SQS.Handle(w, r, region, body, rid)
		return
	}

	if signingService == "ec2" {
		if rt.EC2 == nil {
			http.Error(w, "ec2 service not configured", http.StatusServiceUnavailable)
			return
		}
		ctx := r.Context()
		ctx = WithSigningRegion(ctx, region)
		rt.EC2.Handle(w, r.WithContext(ctx), region, body, rid)
		return
	}

	if !RequireJSONContentType(w, r.Header.Get("Content-Type")) {
		return
	}

	target := r.Header.Get("X-Amz-Target")
	if strings.TrimSpace(target) == "" {
		WriteJSON(w, http.StatusBadRequest, ErrorResponse{
			Type:    "InvalidParameterException",
			Message: "X-Amz-Target header is required",
		})
		return
	}

	dot := strings.IndexByte(target, '.')
	if dot <= 0 || dot >= len(target)-1 {
		WriteJSON(w, http.StatusBadRequest, ErrorResponse{
			Type:    "InvalidParameterException",
			Message: fmt.Sprintf("invalid X-Amz-Target: %q", target),
		})
		return
	}
	serviceName := target[:dot]
	operation := target[dot+1:]

	canonSign := canonicalJSONServiceName(signingService)
	canonTarget := canonicalJSONServiceName(serviceName)
	if canonSign != canonTarget {
		WriteJSON(w, http.StatusBadRequest, ErrorResponse{
			Type:    "InvalidRequestException",
			Message: fmt.Sprintf("the signing credential scope service %q must match the X-Amz-Target service %q (use the same product: e.g. ssm in the scope and AmazonSSM in X-Amz-Target are equivalent)", signingService, serviceName),
		})
		return
	}
	if canonSign != "secretsmanager" && canonSign != "ssm" && canonSign != "lambda" {
		WriteJSON(w, http.StatusBadRequest, ErrorResponse{
			Type:    "InvalidRequestException",
			Message: fmt.Sprintf("unexpected signing service in credential scope: %q (expected secretsmanager, ssm, lambda, or use sqs/iam/ec2 for query APIs)", signingService),
		})
		return
	}

	svc, ok := rt.services[canonTarget]
	if !ok {
		WriteJSON(w, http.StatusBadRequest, ErrorResponse{
			Type:    "InvalidParameterException",
			Message: fmt.Sprintf("unknown or unsupported service: %q", serviceName),
		})
		return
	}

	ctx := r.Context()
	ctx = WithSigningRegion(ctx, region)
	svc.Handle(w, r.WithContext(ctx), operation, body)
}

// Register mounts this router on mux for route "/".
func (rt *Router) Register(mux *http.ServeMux) {
	mux.Handle("/", rt)
}
