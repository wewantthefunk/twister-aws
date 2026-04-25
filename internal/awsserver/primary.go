package awsserver

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/christian/twister/internal/credentials"
	"github.com/christian/twister/internal/s3buckets"
)

// PrimaryHandler routes S3 path-style REST (GET/HEAD/PUT/DELETE /bucket/...) before the JSON 1.1 POST / handler.
type PrimaryHandler struct {
	Provider *credentials.Provider
	S3       *s3buckets.Manager
	API      http.Handler
}

// ServeHTTP dispatches: S3 REST; POST / to JSON 1.1; other methods as appropriate.
func (h *PrimaryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.API == nil {
		http.Error(w, "not configured", http.StatusInternalServerError)
		return
	}
	if h.Provider == nil {
		http.Error(w, "credentials not configured", http.StatusInternalServerError)
		return
	}

	path := r.URL.Path
	if path == "" {
		path = "/"
	}

	// S3 path-style: signing scope must be "s3"; object keys use additional path segments.
	if h.S3 != nil && isS3Method(r.Method) && isS3RESTPath(path) {
		var putBody []byte
		var err error
		if r.Method == http.MethodPut {
			putBody, err = io.ReadAll(io.LimitReader(r.Body, MaxBodyBytes))
			if err != nil {
				http.Error(w, "invalid body", http.StatusBadRequest)
				return
			}
		} else {
			_, _ = io.Copy(io.Discard, r.Body)
		}
		r.Body = io.NopCloser(bytes.NewReader(putBody))

		region, signingService, err := h.Provider.VerifyRequest(r, putBody, time.Now().UTC())
		if err != nil {
			s3buckets.WriteAccessDenied(w, err.Error())
			return
		}
		if signingService != "s3" {
			s3buckets.WriteRESTError(w, http.StatusForbidden, "AccessDenied", "The credential scope service must be s3 for this request.")
			return
		}
		h.S3.HandleS3REST(w, r, region, putBody)
		return
	}

	if r.Method == http.MethodPost && path == "/" {
		h.API.ServeHTTP(w, r)
		return
	}
	if r.Method == http.MethodPost {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func isS3Method(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete:
		return true
	default:
		return false
	}
}

func isS3RESTPath(p string) bool {
	if p == "/health" || p == "/refresh" {
		return false
	}
	if len(p) < 2 || p[0] != '/' {
		return false
	}
	rest := strings.Trim(p, "/")
	return rest != ""
}
