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

// PrimaryHandler routes S3 bucket REST (path-style PUT/DELETE /bucket) before the JSON 1.1 POST / handler.
type PrimaryHandler struct {
	Provider *credentials.Provider
	S3       *s3buckets.Manager
	API      http.Handler
}

// ServeHTTP dispatches: non-POST to / is rejected except where handled by other mux patterns;
// PUT/DELETE on a single top-level path segment to S3; POST / to the JSON 1.1 stack.
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

	// S3 path-style: PUT/DELETE http://endpoint/bucket — signing scope must be "s3".
	if h.S3 != nil && (r.Method == http.MethodPut || r.Method == http.MethodDelete) && isS3SingleBucketPath(path) {
		body, err := io.ReadAll(io.LimitReader(r.Body, MaxBodyBytes))
		if err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))

		_, signingService, err := h.Provider.VerifyRequest(r, body, time.Now().UTC())
		if err != nil {
			s3buckets.WriteAccessDenied(w, err.Error())
			return
		}
		if signingService != "s3" {
			s3buckets.WriteRESTError(w, http.StatusForbidden, "AccessDenied", "The credential scope service must be s3 for this request.")
			return
		}
		h.S3.HandleBucketREST(w, r)
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

func isS3SingleBucketPath(p string) bool {
	if p == "/health" || p == "/refresh" {
		return false
	}
	if len(p) < 2 || p[0] != '/' {
		return false
	}
	rest := p[1:]
	if rest == "" {
		return false
	}
	return !strings.ContainsRune(rest, '/')
}
