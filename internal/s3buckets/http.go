package s3buckets

import (
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// restError is the common S3 REST error body.
type restError struct {
	XMLName   xml.Name `xml:"Error"`
	Code      string   `xml:"Code"`
	Message   string   `xml:"Message"`
	Resource  string   `xml:"Resource,omitempty"`
	RequestID string   `xml:"RequestId,omitempty"`
}

// WriteRESTError writes an S3-style XML error (application/xml).
func WriteRESTError(w http.ResponseWriter, code int, awsCode, message string) {
	if message == "" {
		message = awsCode
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(code)
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	_ = enc.Encode(restError{Code: awsCode, Message: message, RequestID: newRequestID()})
}

// WriteAccessDenied is used when SigV4 fails or key is not allowlisted.
func WriteAccessDenied(w http.ResponseWriter, message string) {
	WriteRESTError(w, http.StatusForbidden, "AccessDenied", message)
}

// HandleBucketREST handles PutBucket and DeleteBucket after authentication.
func (m *Manager) HandleBucketREST(w http.ResponseWriter, r *http.Request) {
	if m == nil {
		http.Error(w, "s3 not configured", http.StatusInternalServerError)
		return
	}
	rid := newRequestID()
	w.Header().Set("x-amz-request-id", rid)

	bucket, err := parseTopLevelBucketPath(r.URL.Path)
	if err != nil {
		WriteRESTError(w, http.StatusBadRequest, "InvalidRequest", err.Error())
		return
	}

	switch r.Method {
	case http.MethodPut:
		err := m.CreateBucket(bucket)
		switch {
		case err == nil:
			w.WriteHeader(http.StatusOK)
		case errors.Is(err, ErrInvalidBucketName):
			WriteRESTError(w, http.StatusBadRequest, "InvalidBucketName", "The specified bucket is not valid.")
		case errors.Is(err, ErrBucketAlreadyExists):
			WriteRESTError(w, http.StatusConflict, "BucketAlreadyOwnedByYou", "Your previous request to create the named bucket succeeded and you already own it.")
		default:
			WriteRESTError(w, http.StatusInternalServerError, "InternalError", err.Error())
		}
		return
	case http.MethodDelete:
		err := m.DeleteBucket(bucket)
		switch {
		case err == nil:
			w.WriteHeader(http.StatusNoContent)
		case errors.Is(err, ErrInvalidBucketName):
			WriteRESTError(w, http.StatusBadRequest, "InvalidBucketName", "The specified bucket is not valid.")
		case errors.Is(err, ErrNoSuchBucket):
			WriteRESTError(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist.")
		case errors.Is(err, ErrBucketNotEmpty):
			WriteRESTError(w, http.StatusConflict, "BucketNotEmpty", "The bucket you tried to delete is not empty.")
		default:
			WriteRESTError(w, http.StatusInternalServerError, "InternalError", err.Error())
		}
		return
	default:
		w.Header().Set("Allow", "PUT, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func parseTopLevelBucketPath(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("empty path")
	}
	// r.URL.Path is unescaped; still normalize
	if p[0] != '/' {
		return "", fmt.Errorf("path must be absolute")
	}
	p = p[1:]
	if p == "" {
		return "", fmt.Errorf("missing bucket name")
	}
	if strings.Contains(p, "/") {
		return "", fmt.Errorf("use bucket-only path for this operation; object paths are not implemented")
	}
	if p == "." || p == ".." {
		return "", fmt.Errorf("invalid bucket name")
	}
	return p, nil
}
