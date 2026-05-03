package awsserver

import "testing"

func TestIsS3RESTPath(t *testing.T) {
	if isS3RESTPath("/health") || isS3RESTPath("/refresh") {
		t.Fatal("admin paths not s3")
	}
	if !isS3RESTPath("/my-bucket") {
		t.Fatal("bucket path")
	}
	if !isS3RESTPath("/first-bucket/go.mod") {
		t.Fatal("object path")
	}
	if isS3RESTPath("/") {
		t.Fatal("root")
	}
	if isS3RESTPath("") {
		t.Fatal("empty")
	}
}

func TestPrimaryHandlerMaxS3PutBodyBytes(t *testing.T) {
	var nilHandler *PrimaryHandler
	if got := nilHandler.maxS3PutBodyBytes(); got != defaultMaxS3PutBodyBytes {
		t.Fatalf("nil handler: got %d want %d", got, defaultMaxS3PutBodyBytes)
	}

	h := &PrimaryHandler{}
	if got := h.maxS3PutBodyBytes(); got != defaultMaxS3PutBodyBytes {
		t.Fatalf("zero override: got %d want %d", got, defaultMaxS3PutBodyBytes)
	}

	h.MaxS3PutBodyBytes = 2 << 20
	if got := h.maxS3PutBodyBytes(); got != 2<<20 {
		t.Fatalf("custom override: got %d want %d", got, 2<<20)
	}
}
