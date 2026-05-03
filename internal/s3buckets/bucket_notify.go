package s3buckets

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Notification storage lives outside bucket object dirs: Root/.s3-notifications/region/bucket.json

func (m *Manager) notificationFilePath(region, bucket string) (string, error) {
	if !IsValidRegionSegment(NormalizeRegion(region)) {
		return "", ErrInvalidRegion
	}
	if !IsValidBucketName(bucket) {
		return "", ErrInvalidBucketName
	}
	if m == nil || m.Root == "" {
		return "", errors.New("s3: empty root")
	}
	r := NormalizeRegion(region)
	return filepath.Join(m.Root, ".s3-notifications", r, bucket+".json"), nil
}

// bucketNotification is the on-disk form (and JSON request alternative).
type bucketNotification struct {
	QueueConfigs []queueNotification `json:"queueConfigurations"`
}

type queueNotification struct {
	ID       string   `json:"id,omitempty"`
	QueueARN string   `json:"queueArn"`
	Events   []string `json:"events"`
}

// --- query ---

func isNotificationSubrequest(r *http.Request) bool {
	_, ok := r.URL.Query()["notification"]
	return ok
}

// PutBucketNotification stores configuration from the request body.
func (m *Manager) putBucketNotification(w http.ResponseWriter, r *http.Request, region, bucket string, putBody []byte) {
	if _, err := os.Stat(m.BucketPath(region, bucket)); err != nil {
		if os.IsNotExist(err) {
			WriteRESTError(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist.")
			return
		}
		WriteRESTError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	p, err := m.notificationFilePath(region, bucket)
	if err != nil {
		WriteRESTError(w, http.StatusBadRequest, "InvalidRequest", err.Error())
		return
	}
	ct := strings.ToLower(r.Header.Get("Content-Type"))
	var cfg *bucketNotification
	if strings.HasPrefix(ct, "application/json") {
		parsed, err := parseNotificationJSON(putBody)
		if err != nil {
			WriteRESTError(w, http.StatusBadRequest, "InvalidArgument", err.Error())
			return
		}
		cfg = parsed
	} else {
		// S3 and AWS CLI use application/xml; empty means clear.
		if len(bytes.TrimSpace(putBody)) == 0 {
			cfg = &bucketNotification{}
		} else {
			var x notifConfigXML
			if err := xml.Unmarshal(putBody, &x); err != nil {
				WriteRESTError(w, http.StatusBadRequest, "InvalidArgument", err.Error())
				return
			}
			cfg = notifConfigFromXML(&x)
		}
	}
	if err := m.saveNotificationConfig(p, cfg); err != nil {
		WriteRESTError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (m *Manager) getBucketNotification(w http.ResponseWriter, region, bucket string) {
	p, err := m.notificationFilePath(region, bucket)
	if err != nil {
		WriteRESTError(w, http.StatusBadRequest, "InvalidRequest", err.Error())
		return
	}
	if _, err := os.Stat(m.BucketPath(region, bucket)); err != nil {
		if os.IsNotExist(err) {
			WriteRESTError(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist.")
			return
		}
		WriteRESTError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	cfg, err := m.loadNotificationConfig(p)
	if err != nil {
		WriteRESTError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	enc := xml.NewEncoder(&buf)
	enc.Indent("", "  ")
	out := notifConfigToXML(cfg)
	_ = enc.Encode(&out)
	_ = enc.Flush()
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

// XML shapes for AWS S3 (subset).
type notifConfigXML struct {
	XMLName             xml.Name              `xml:"NotificationConfiguration"`
	Xmlns               string                `xml:"xmlns,attr,omitempty"`
	QueueConfigurations []notifQueueConfigXML `xml:"QueueConfiguration"`
}

type notifQueueConfigXML struct {
	ID     string   `xml:"Id"`
	Queue  string   `xml:"Queue"`
	Events []string `xml:"Event"`
}

func notifConfigFromXML(x *notifConfigXML) *bucketNotification {
	out := &bucketNotification{}
	for _, q := range x.QueueConfigurations {
		out.QueueConfigs = append(out.QueueConfigs, queueNotification{
			ID:       q.ID,
			QueueARN: q.Queue,
			Events:   q.Events,
		})
	}
	return out
}

func notifConfigToXML(c *bucketNotification) notifConfigXML {
	var x notifConfigXML
	x.XMLName = xml.Name{Local: "NotificationConfiguration"}
	x.Xmlns = "http://s3.amazonaws.com/doc/2006-03-01/"
	for _, q := range c.QueueConfigs {
		x.QueueConfigurations = append(x.QueueConfigurations, notifQueueConfigXML{
			ID:     q.ID,
			Queue:  q.QueueARN,
			Events: q.Events,
		})
	}
	return x
}

var notificationMu sync.Mutex

func (m *Manager) loadNotificationConfig(path string) (*bucketNotification, error) {
	notificationMu.Lock()
	defer notificationMu.Unlock()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &bucketNotification{}, nil
		}
		return nil, err
	}
	var c bucketNotification
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func (m *Manager) saveNotificationConfig(path string, c *bucketNotification) error {
	notificationMu.Lock()
	defer notificationMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// fireObjectCreated dispatches s3:ObjectCreated events to SQS (best effort).
func (m *Manager) fireObjectCreated(bucketRegion, bucket, objectKey string, size int64, etag string) {
	if m == nil || m.Events == nil {
		return
	}
	p, err := m.notificationFilePath(bucketRegion, bucket)
	if err != nil {
		return
	}
	cfg, err := m.loadNotificationConfig(p)
	if err != nil || cfg == nil {
		return
	}
	for _, q := range cfg.QueueConfigs {
		if !eventMatchesObjectPut(q.Events) {
			continue
		}
		sqsr, name, err := parseSQSQueueARN(q.QueueARN)
		if err != nil {
			continue
		}
		body, err := BuildObjectCreatedEventJSON(bucketRegion, bucket, objectKey, size, etag, q.ID)
		if err != nil {
			continue
		}
		_ = m.Events.EnqueueS3Event(sqsr, name, body)
	}
}

func eventMatchesObjectPut(events []string) bool {
	for _, e := range events {
		t := strings.TrimSpace(e)
		if t == "s3:ObjectCreated:*" || t == "s3:ObjectCreated:Put" {
			return true
		}
	}
	return false
}

// parseSQSQueueARN: arn:aws:sqs:region:account:queueName (queueName may contain ":")
func parseSQSQueueARN(arn string) (region, queueName string, err error) {
	arn = strings.TrimSpace(arn)
	parts := strings.Split(arn, ":")
	if len(parts) < 6 || parts[0] != "arn" || parts[1] != "aws" || parts[2] != "sqs" {
		return "", "", fmt.Errorf("invalid SQS queue ARN: %q", arn)
	}
	region = parts[3]
	queueName = strings.Join(parts[5:], ":")
	return region, queueName, nil
}

// parseNotificationJSON accepts Twister's on-disk shape (queueConfigurations / queueArn)
// or the AWS API shape (QueueConfigurations / QueueArn) used in the CLI --notification-configuration JSON.
func parseNotificationJSON(b []byte) (*bucketNotification, error) {
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return &bucketNotification{}, nil
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err != nil {
		return nil, err
	}
	if _, isAWS := top["QueueConfigurations"]; isAWS {
		var awsShape struct {
			QC []struct {
				ID       string   `json:"Id"`
				QueueArn string   `json:"QueueArn"`
				Events   []string `json:"Events"`
			} `json:"QueueConfigurations"`
		}
		if err := json.Unmarshal(b, &awsShape); err != nil {
			return nil, err
		}
		out := &bucketNotification{}
		for _, q := range awsShape.QC {
			out.QueueConfigs = append(out.QueueConfigs, queueNotification{
				ID:       q.ID,
				QueueARN: q.QueueArn,
				Events:   q.Events,
			})
		}
		return out, nil
	}
	var tw bucketNotification
	if err := json.Unmarshal(b, &tw); err != nil {
		return nil, err
	}
	return &tw, nil
}
