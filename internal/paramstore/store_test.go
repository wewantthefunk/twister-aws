package paramstore

import (
	"testing"
	"time"
)

func TestNameAndRegionFromID_plainName(t *testing.T) {
	n, r := NameAndRegionFromID("/app/foo")
	if n != "/app/foo" || r != "" {
		t.Fatalf("got name=%q region=%q", n, r)
	}
}

func TestNameAndRegionFromID_ssmARN(t *testing.T) {
	arn := "arn:aws:ssm:us-west-2:000000000000:parameter/twister/demo"
	n, r := NameAndRegionFromID(arn)
	if r != "us-west-2" {
		t.Fatalf("region: %q", r)
	}
	if n != "/twister/demo" {
		t.Fatalf("name: %q", n)
	}
}

func TestStore_LookupInRegion_ARNRegionMismatch(t *testing.T) {
	s := NewStore()
	s.Put(&ParameterRecord{Region: "us-west-2", Name: "/twister/demo", Type: "String", Value: "v", Version: 1, LastModified: time.Unix(1, 0).UTC()})
	arn := "arn:aws:ssm:us-west-2:000000000000:parameter/twister/demo"
	if s.LookupInRegion(arn, "us-east-1") != nil {
		t.Fatal("expected nil when request region != ARN region")
	}
}

func TestStore_LookupInRegion_byName(t *testing.T) {
	s := NewStore()
	s.Put(&ParameterRecord{Region: "us-east-1", Name: "/a", Type: "String", Value: "1", Version: 1, LastModified: time.Unix(1, 0).UTC()})
	if s.LookupInRegion("/a", "us-east-1") == nil {
		t.Fatal("expected record")
	}
}
