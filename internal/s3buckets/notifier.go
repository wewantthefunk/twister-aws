package s3buckets

// S3EventSink enqueues a JSON S3 event notification to an SQS queue. Implemented
// in main (wrapping *sqs.Manager) to avoid a package import cycle.
type S3EventSink interface {
	EnqueueS3Event(sqsRegion, queueName, eventJSON string) error
}
