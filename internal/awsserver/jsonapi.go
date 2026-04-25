package awsserver

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Wire constants for the AWS JSON 1.1 protocol.
const (
	// MaxBodyBytes limits POST body size.
	MaxBodyBytes = 1 << 20
	// ContentTypeJSON is application/x-amz-json-1.1.
	ContentTypeJSON = "application/x-amz-json-1.1"
)

// ErrorResponse is a common __type + message error shape.
type ErrorResponse struct {
	Type    string `json:"__type"`
	Message string `json:"message"`
}

// WriteJSON writes a JSON 1.1 response with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", ContentTypeJSON)
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// NewRequestID returns a value suitable for x-amzn-RequestId.
func NewRequestID() string {
	var b [16]byte
	if _, err := io.ReadFull(rand.Reader, b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// RequireJSONContentType returns false and writes an error if Content-Type is not x-amz-json-1.1.
func RequireJSONContentType(w http.ResponseWriter, ct string) bool {
	if strings.HasPrefix(ct, ContentTypeJSON) {
		return true
	}
	WriteJSON(w, http.StatusBadRequest, ErrorResponse{
		Type:    "InvalidParameterException",
		Message: "Content-Type must be application/x-amz-json-1.1",
	})
	return false
}
