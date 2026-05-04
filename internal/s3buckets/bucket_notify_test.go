package s3buckets

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestParseSQSQueueARN(t *testing.T) {
	r, q, err := parseSQSQueueARN("arn:aws:sqs:us-east-1:000000000000:first-queue")
	if err != nil || r != "us-east-1" || q != "first-queue" {
		t.Fatalf("got %q %q err %v", r, q, err)
	}
}

func TestEventMatchesObjectPut(t *testing.T) {
	if !eventMatchesObjectPut([]string{"s3:ObjectCreated:*"}) {
		t.Fatal("wildcard")
	}
	if !eventMatchesObjectPut([]string{"s3:ObjectCreated:Put"}) {
		t.Fatal("put")
	}
	if eventMatchesObjectPut([]string{"s3:ObjectRemoved:*"}) {
		t.Fatal("should not match")
	}
}

func TestNotifConfigXML(t *testing.T) {
	const in = `<NotificationConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
<QueueConfiguration>
<Id>test</Id>
<Queue>arn:aws:sqs:us-east-1:000000000000:q1</Queue>
<Event>s3:ObjectCreated:*</Event>
</QueueConfiguration>
</NotificationConfiguration>`
	var x notifConfigXML
	if err := xml.Unmarshal([]byte(in), &x); err != nil {
		t.Fatal(err)
	}
	cfg := notifConfigFromXML(&x)
	if len(cfg.QueueConfigs) != 1 || cfg.QueueConfigs[0].QueueARN == "" {
		t.Fatalf("%#v", cfg)
	}
}

type captureSink struct {
	lastRegion, lastQueue, lastBody string
}

func (c *captureSink) EnqueueS3Event(sqsRegion, queueName, eventJSON string) error {
	c.lastRegion, c.lastQueue, c.lastBody = sqsRegion, queueName, eventJSON
	return nil
}

func TestFireObjectCreated(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)
	if err := m.CreateBucket("us-east-1", "bkt"); err != nil {
		t.Fatal(err)
	}
	p, err := m.notificationFilePath("us-east-1", "bkt")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.saveNotificationConfig(p, &bucketNotification{
		QueueConfigs: []queueNotification{{
			ID:       "n1",
			QueueARN: "arn:aws:sqs:us-east-1:000000000000:q1",
			Events:   []string{"s3:ObjectCreated:*"},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	s := &captureSink{}
	m.Events = s
	if err := m.PutObject("us-east-1", "bkt", "a.txt", []byte("hi")); err != nil {
		t.Fatal(err)
	}
	if s.lastQueue != "q1" || s.lastRegion != "us-east-1" {
		t.Fatalf("region=%q queue=%q", s.lastRegion, s.lastQueue)
	}
	if !strings.Contains(s.lastBody, "ObjectCreated:Put") || !strings.Contains(s.lastBody, "a.txt") {
		t.Fatalf("body: %s", s.lastBody)
	}
}
