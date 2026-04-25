package awsserver

import "testing"

func Test_canonicalJSONServiceName(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"ssm", "ssm"},
		{"AmazonSSM", "ssm"},
		{"amazonssm", "ssm"},
		{"secretsmanager", "secretsmanager"},
		{"SecretsManager", "secretsmanager"},
	}
	for _, tc := range tests {
		if got := canonicalJSONServiceName(tc.in); got != tc.want {
			t.Fatalf("canonicalJSONServiceName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
