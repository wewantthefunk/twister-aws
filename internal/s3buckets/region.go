package s3buckets

import (
	"regexp"
	"strings"
)

// DefaultRegion is used when the request omits a region (should not happen after successful SigV4).
const DefaultRegion = "us-east-1"

// NormalizeRegion lowercases and trims; empty string becomes DefaultRegion.
func NormalizeRegion(s string) string {
	t := strings.ToLower(strings.TrimSpace(s))
	if t == "" {
		return DefaultRegion
	}
	return t
}

// AWS-style region names for a single path segment (us-east-1, ap-south-1, …).
var regionSegmentPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*[a-z0-9]$|^[a-z0-9]{1,2}$`)

// IsValidRegionSegment checks that a normalized region is safe as one directory name.
func IsValidRegionSegment(s string) bool {
	if s == "" || len(s) > 32 {
		return false
	}
	return regionSegmentPattern.MatchString(s) && s != ".." && s != "."
}
