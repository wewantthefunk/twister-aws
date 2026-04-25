package iam

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/christian/twister/internal/credentials"
)

// Service handles IAM Query API requests (form-encoded) after SigV4 verification.
type Service struct {
	Provider *credentials.Provider
}

// New returns an IAM service that persists new access keys through provider.
func New(provider *credentials.Provider) *Service {
	return &Service{Provider: provider}
}

const xmlNS = "https://iam.amazonaws.com/doc/2010-05-08/"

// Handle implements the IAM portion of the AWS single-endpoint flow (POST /, form body).
func (s *Service) Handle(w http.ResponseWriter, r *http.Request, body []byte, requestID string) {
	if s == nil || s.Provider == nil {
		http.Error(w, "not configured", http.StatusInternalServerError)
		return
	}

	ct := r.Header.Get("Content-Type")
	if !isFormCT(ct) {
		writeXMLError(w, http.StatusBadRequest, "InvalidParameterValue", "Content-Type must be application/x-www-form-urlencoded", requestID)
		return
	}

	q, err := url.ParseQuery(string(body))
	if err != nil {
		writeXMLError(w, http.StatusBadRequest, "MalformedQueryString", "invalid form body", requestID)
		return
	}

	switch strings.TrimSpace(q.Get("Action")) {
	case "CreateAccessKey":
		if v := q.Get("Version"); v != "" && v != "2010-05-08" {
			writeXMLError(w, http.StatusBadRequest, "InvalidInput", fmt.Sprintf("unsupported Version %q (expected 2010-05-08)", v), requestID)
			return
		}
		s.createAccessKey(w, requestID)
	default:
		act := q.Get("Action")
		if act == "" {
			writeXMLError(w, http.StatusBadRequest, "InvalidQueryParameter", "Action is required", requestID)
			return
		}
		writeXMLError(w, http.StatusBadRequest, "InvalidAction", "unsupported Action "+act, requestID)
	}
}

func isFormCT(ct string) bool {
	ct = strings.ToLower(ct)
	return strings.HasPrefix(ct, "application/x-www-form-urlencoded")
}

// IsBootstrapCreateAccessKey reports whether the request looks like IAM CreateAccessKey on the Query API.
// The router uses it when the credential allowlist is empty: SigV4 cannot be verified without a known secret,
// so the first key is created without a prior server-side key (same as “no secrets file” for empty stores).
func IsBootstrapCreateAccessKey(r *http.Request, body []byte) bool {
	if r == nil || !isFormCT(r.Header.Get("Content-Type")) {
		return false
	}
	q, err := url.ParseQuery(string(body))
	if err != nil {
		return false
	}
	return strings.TrimSpace(q.Get("Action")) == "CreateAccessKey"
}

func (s *Service) createAccessKey(w http.ResponseWriter, requestID string) {
	accessKey, secret, err := newKeyPair()
	if err != nil {
		writeXMLError(w, http.StatusInternalServerError, "ServiceFailure", err.Error(), requestID)
		return
	}
	if err := s.Provider.AddAccessKeyAndPersist(accessKey, secret); err != nil {
		writeXMLError(w, http.StatusInternalServerError, "ServiceFailure", err.Error(), requestID)
		return
	}

	created := time.Now().UTC()
		resp := createAccessKeyResult{
		XMLName: xml.Name{Local: "CreateAccessKeyResponse"},
		Xmlns:   xmlNS,
		CreateAccessKeyResult: cakrInner{
			AccessKey: keyXML{
				UserName:        "default",
				AccessKeyId:     accessKey,
				Status:          "Active",
				SecretAccessKey: secret,
				CreateDate:      created,
			},
		},
		ResponseMetadata: responseMetadata{RequestId: requestID},
	}
	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	enc := xml.NewEncoder(&buf)
	enc.Indent("", "  ")
	if err := enc.Encode(resp); err != nil {
		writeXMLError(w, http.StatusInternalServerError, "ServiceFailure", err.Error(), requestID)
		return
	}
	_ = enc.Flush()
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

// newKeyPair returns an access key (AKIA + 16 hex) and 40 character secret.
func newKeyPair() (accessKeyID, secretAccessKey string, err error) {
	ak, err := randomAKIA()
	if err != nil {
		return "", "", err
	}
	sk, err := randomSecret40()
	if err != nil {
		return "", "", err
	}
	return ak, sk, nil
}

func randomAKIA() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "AKIA" + strings.ToUpper(hex.EncodeToString(b)), nil
}

func randomSecret40() (string, error) {
	b := make([]byte, 30)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	// 40 character secret (AWS style).
	return base64.RawStdEncoding.EncodeToString(b), nil
}

// XML shapes (subset of AWS IAM CreateAccessKey response).
type createAccessKeyResult struct {
	XMLName               xml.Name `xml:"CreateAccessKeyResponse"`
	Xmlns                 string   `xml:"xmlns,attr"`
	CreateAccessKeyResult cakrInner `xml:"CreateAccessKeyResult"`
	ResponseMetadata      responseMetadata `xml:"ResponseMetadata"`
}

type cakrInner struct {
	AccessKey keyXML `xml:"AccessKey"`
}

type keyXML struct {
	UserName        string    `xml:"UserName"`
	AccessKeyId     string    `xml:"AccessKeyId"`
	Status          string    `xml:"Status"`
	SecretAccessKey string    `xml:"SecretAccessKey"`
	CreateDate      time.Time `xml:"CreateDate"`
}

type responseMetadata struct {
	RequestId string `xml:"RequestId"`
}

type errRespXML struct {
	XMLName   xml.Name `xml:"ErrorResponse"`
	Xmlns     string   `xml:"xmlns,attr"`
	Error     errBody  `xml:"Error"`
	RequestId string   `xml:"RequestId"`
}

type errBody struct {
	Type    string `xml:"Type"`
	Code    string `xml:"Code"`
	Message string `xml:"Message"`
}

func writeXMLError(w http.ResponseWriter, status int, code, message, requestID string) {
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.WriteHeader(status)
	er := errRespXML{
		XMLName: xml.Name{Local: "ErrorResponse"},
		Xmlns:   xmlNS,
		Error: errBody{
			Type:    "Sender",
			Code:    code,
			Message: message,
		},
		RequestId: requestID,
	}
	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	enc := xml.NewEncoder(&buf)
	_ = enc.Encode(&er)
	_ = enc.Flush()
	_, _ = w.Write(buf.Bytes())
}