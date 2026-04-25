package sqs

import "regexp"

// IsValidQueueName enforces a subset of SQS rules: 1–80 chars, alphanumeric, hyphens, underscores.
var queueNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,80}$`)

func IsValidQueueName(s string) bool {
	return queueNamePattern.MatchString(s)
}
