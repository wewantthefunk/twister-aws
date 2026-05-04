package s3buckets

import (
	"regexp"
	"strings"
)

var bucketNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*[a-z0-9]$`)

// IsValidBucketName enforces a subset of S3 DNS–style rules (3–63 characters).
func IsValidBucketName(s string) bool {
	if len(s) < 3 || len(s) > 63 {
		return false
	}
	if strings.Contains(s, "..") {
		return false
	}
	return bucketNamePattern.MatchString(s)
}
