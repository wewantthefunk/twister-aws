package lambda

import (
	"testing"
)

func TestEventSourceStore_AddFindDelete(t *testing.T) {
	root := t.TempDir()
	s := NewEventSourceStore(root)
	m := EventSourceMapping{
		UUID:           "u1",
		EventSourceArn: "arn:aws:sqs:us-east-1:000000000000:q1",
		FunctionName:   "f1",
		State:          "Enabled",
		BatchSize:      1,
	}
	if err := s.AddEventSourceMapping(m); err != nil {
		t.Fatal(err)
	}
	fn, err := s.FindFunctionForSQS("us-east-1", "q1")
	if err != nil {
		t.Fatal(err)
	}
	if fn != "f1" {
		t.Fatalf("FindFunctionForSQS: %q", fn)
	}
	fn2, err := s.FindFunctionForSQS("us-west-2", "q1")
	if err != nil {
		t.Fatal(err)
	}
	if fn2 != "" {
		t.Fatalf("different region: expected no match, got %q", fn2)
	}
	if err := s.DeleteByUUID("u1"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteByUUID("u1"); err == nil {
		t.Fatal("expected error")
	}
	list, err := s.ListEventSourceMappings()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("expected no mappings, got %d", len(list))
	}
}
