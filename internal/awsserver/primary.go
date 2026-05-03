package awsserver

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/christian/twister/internal/credentials"
	"github.com/christian/twister/internal/s3buckets"
)

const defaultMaxS3PutBodyBytes int64 = 16 << 20 // 16 MiB

// PrimaryHandler routes S3 path-style REST (GET/HEAD/PUT/DELETE /bucket/...) before the JSON 1.1 POST / handler.
type PrimaryHandler struct {
	Provider *credentials.Provider
	S3       *s3buckets.Manager
	API      http.Handler
	// MaxS3PutBodyBytes sets the max object size accepted by S3 PUT.
	// Zero or negative uses defaultMaxS3PutBodyBytes.
	MaxS3PutBodyBytes int64
}

func (h *PrimaryHandler) maxS3PutBodyBytes() int64 {
	if h == nil || h.MaxS3PutBodyBytes <= 0 {
		return defaultMaxS3PutBodyBytes
	}
	return h.MaxS3PutBodyBytes
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
			maxBytes := h.maxS3PutBodyBytes()
			putBody, err = io.ReadAll(io.LimitReader(r.Body, maxBytes+1))
			if err != nil {
				http.Error(w, "invalid body", http.StatusBadRequest)
				return
			}
			if int64(len(putBody)) > maxBytes {
				s3buckets.WriteRESTError(
					w,
					http.StatusBadRequest,
					"EntityTooLarge",
					fmt.Sprintf("Your proposed upload exceeds the maximum allowed object size (%d bytes).", maxBytes),
				)
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
