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
