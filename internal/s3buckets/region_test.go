package s3buckets

import "testing"

func TestNormalizeRegion(t *testing.T) {
	if NormalizeRegion("") != DefaultRegion {
		t.Fatal()
	}
	if NormalizeRegion("  US-WEST-2  ") != "us-west-2" {
		t.Fatal()
	}
}

func TestIsValidRegionSegment(t *testing.T) {
	for _, r := range []string{"us-east-1", "ap-south-1", "eu-west-1", "ca-central-1"} {
		if !IsValidRegionSegment(r) {
			t.Fatalf("should be valid: %q", r)
		}
	}
	if IsValidRegionSegment("../x") {
		t.Fatal("path escape")
	}
}
