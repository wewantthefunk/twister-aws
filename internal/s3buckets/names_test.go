package s3buckets

import "testing"

func TestIsValidBucketName(t *testing.T) {
	if IsValidBucketName("ab") {
		t.Fatal("too short")
	}
	if !IsValidBucketName("my-bucket-01") {
		t.Fatal()
	}
	if IsValidBucketName("My-Bucket") {
		t.Fatal("uppercase")
	}
	if IsValidBucketName("a..b") {
		t.Fatal("..")
	}
}
