package awsserver

import "testing"

func TestIsS3SingleBucketPath(t *testing.T) {
	if isS3SingleBucketPath("/health") || isS3SingleBucketPath("/refresh") {
		t.Fatal("admin paths not s3")
	}
	if !isS3SingleBucketPath("/my-bucket") {
		t.Fatal("bucket path")
	}
	if isS3SingleBucketPath("/a/b") {
		t.Fatal("object path")
	}
	if isS3SingleBucketPath("/") {
		t.Fatal("root")
	}
}
