package s3buckets

import (
	"encoding/json"
	"fmt"
	"time"
)

// s3EventEnvelope matches the usual S3 → SQS notification JSON shape.
type s3EventEnvelope struct {
	Records []s3EventRecord `json:"Records"`
}

type s3EventRecord struct {
	EventVersion string `json:"eventVersion"`
	EventSource  string `json:"eventSource"`
	AwsRegion    string `json:"awsRegion"`
	EventTime    string `json:"eventTime"`
	EventName    string `json:"eventName"`
	S3           s3Data `json:"s3"`
}

type s3Data struct {
	SchemaVersion   string   `json:"s3SchemaVersion"`
	ConfigurationID string   `json:"configurationId,omitempty"`
	Bucket          s3Bucket `json:"bucket"`
	Object          s3Object `json:"object"`
}

type s3Bucket struct {
	Name string `json:"name"`
	ARN  string `json:"arn"`
}

type s3Object struct {
	Key          string `json:"key"`
	Size         int64  `json:"size"`
	ETag         string `json:"eTag"`
	Sequencer    string `json:"sequencer,omitempty"`
}

// BuildObjectCreatedEventJSON returns the JSON body SQS receives for a single Put.
func BuildObjectCreatedEventJSON(bucketRegion, bucket, objectKey string, size int64, etag, configurationID string) (string, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	// eTag in events is often without quotes; our ETag is hex
	rec := s3EventRecord{
		EventVersion: "2.1",
		EventSource:  "aws:s3",
		AwsRegion:    bucketRegion,
		EventTime:    now,
		EventName:    "ObjectCreated:Put",
		S3: s3Data{
			SchemaVersion:   "1.0",
			ConfigurationID: configurationID,
			Bucket: s3Bucket{
				Name: bucket,
				ARN:  fmt.Sprintf("arn:aws:s3:::%s", bucket),
			},
			Object: s3Object{
				Key:  objectKey,
				Size: size,
				ETag: etag,
			},
		},
	}
	env := s3EventEnvelope{Records: []s3EventRecord{rec}}
	b, err := json.Marshal(env)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
