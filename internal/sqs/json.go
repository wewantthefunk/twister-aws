package sqs

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/christian/twister/internal/s3buckets"
)

// AWS CLI v2+ uses the Amazon SQS JSON protocol by default: Content-Type application/x-amz-json-1.0
// and the X-Amz-Target: AmazonSQS.CreateQueue (etc.) header. See:
// https://docs.aws.amazon.com/AWSSimpleQueueService/latest/SQSDeveloperGuide/sqs-making-api-requests-json.html

const contentTypeAmzJSON1 = "application/x-amz-json-1.0"

func isAmzJSON1(lowerContentType string) bool {
	// e.g. application/x-amz-json-1.0; charset=utf-8
	return strings.HasPrefix(lowerContentType, "application/x-amz-json-1.0")
}

func parseAmzTargetOp(target string) (op string) {
	s := strings.TrimSpace(target)
	if s == "" {
		return ""
	}
	// "com.amazonaws.sqs#CreateQueue" (Smithy)
	if i := strings.Index(s, "#"); i >= 0 {
		return s[i+1:]
	}
	// "AmazonSQS.CreateQueue" (per AWS docs)
	if i := strings.LastIndex(s, "."); i >= 0 {
		return s[i+1:]
	}
	return ""
}

// handleAmzJSON implements the AWS SQS JSON 1.0 protocol.
func (s *Service) handleAmzJSON(w http.ResponseWriter, r *http.Request, body []byte, requestID, region string) {
	target := r.Header.Get("X-Amz-Target")
	op := parseAmzTargetOp(target)
	if op == "" {
		writeJSON1Err(w, http.StatusBadRequest, "com.amazonaws.sqs#MissingAction", "X-Amz-Target header is required (e.g. AmazonSQS.CreateQueue)", requestID)
		return
	}

	region = s3buckets.NormalizeRegion(region)
	if !s3buckets.IsValidRegionSegment(region) {
		writeJSON1Err(w, http.StatusBadRequest, "com.amazonaws.sqs#InvalidParameter", "invalid signing region in credential scope", requestID)
		return
	}
	base := requestBaseURL(r)

	switch op {
	case "CreateQueue":
		s.jsonCreateQueue(w, body, region, base, requestID)
	case "GetQueueUrl":
		s.jsonGetQueueURL(w, body, region, base, requestID)
	case "ListQueues":
		s.jsonListQueues(w, body, region, base, requestID)
	case "SendMessage":
		s.jsonSendMessage(w, body, region, requestID)
	case "ReceiveMessage":
		s.jsonReceiveMessage(w, body, region, requestID)
	case "DeleteMessage":
		s.jsonDeleteMessage(w, body, region, requestID)
	case "PurgeQueue":
		s.jsonPurgeQueue(w, body, region, requestID)
	default:
		writeJSON1Err(w, http.StatusBadRequest, "com.amazonaws.sqs#InvalidAction", "unsupported X-Amz-Target operation: "+op, requestID)
	}
}

// --- request bodies ---

type jCreateQueue struct {
	QueueName string `json:"QueueName"`
}

type jGetQueueUrl struct {
	QueueName string `json:"QueueName"`
}

type jListQueues struct {
	QueueNamePrefix string `json:"QueueNamePrefix"`
}

type jSendMessage struct {
	QueueUrl    string `json:"QueueUrl"`
	MessageBody string `json:"MessageBody"`
}

type jReceiveMessage struct {
	QueueUrl            string `json:"QueueUrl"`
	MaxNumberOfMessages *int   `json:"MaxNumberOfMessages"`
	WaitTimeSeconds     *int   `json:"WaitTimeSeconds"`
	VisibilityTimeout   *int   `json:"VisibilityTimeout"`
}

type jDeleteMessage struct {
	QueueUrl       string `json:"QueueUrl"`
	ReceiptHandle  string `json:"ReceiptHandle"`
}

type jPurgeQueue struct {
	QueueUrl string `json:"QueueUrl"`
}

type jMessageOut struct {
	MessageId      string `json:"MessageId"`
	ReceiptHandle  string `json:"ReceiptHandle"`
	MD5OfBody      string `json:"MD5OfBody"`
	Body           string `json:"Body"`
}

// --- operations ---

func (s *Service) jsonCreateQueue(w http.ResponseWriter, body []byte, region, base, requestID string) {
	var in jCreateQueue
	if err := json.Unmarshal(body, &in); err != nil {
		writeJSON1Err(w, http.StatusBadRequest, "com.amazonaws.sqs#MalformedInput", err.Error(), requestID)
		return
	}
	if in.QueueName == "" {
		writeJSON1Err(w, http.StatusBadRequest, "com.amazonaws.sqs#MissingParameter", "QueueName is required", requestID)
		return
	}
	if !IsValidQueueName(in.QueueName) {
		writeJSON1Err(w, http.StatusBadRequest, "com.amazonaws.sqs#InvalidParameter", "Invalid queue name", requestID)
		return
	}
	if err := s.Manager.CreateQueue(region, in.QueueName); err != nil {
		mapManagerErrJSON(w, err, requestID)
		return
	}
	qu := s.makeQueueURL(base, region, in.QueueName)
	w.Header().Set("x-amzn-RequestId", requestID)
	writeJSON1(w, http.StatusOK, struct {
		QueueUrl string `json:"QueueUrl"`
	}{QueueUrl: qu})
}

func (s *Service) jsonGetQueueURL(w http.ResponseWriter, body []byte, region, base, requestID string) {
	var in jGetQueueUrl
	if err := json.Unmarshal(body, &in); err != nil {
		writeJSON1Err(w, http.StatusBadRequest, "com.amazonaws.sqs#MalformedInput", err.Error(), requestID)
		return
	}
	if in.QueueName == "" {
		writeJSON1Err(w, http.StatusBadRequest, "com.amazonaws.sqs#MissingParameter", "QueueName is required", requestID)
		return
	}
	ok, err := s.Manager.QueueHas(region, in.QueueName)
	if err != nil {
		mapManagerErrJSON(w, err, requestID)
		return
	}
	if !ok {
		writeJSON1Err(w, http.StatusBadRequest, "com.amazonaws.sqs#QueueDoesNotExist", "The specified queue does not exist.", requestID)
		return
	}
	qu := s.makeQueueURL(base, region, in.QueueName)
	w.Header().Set("x-amzn-RequestId", requestID)
	writeJSON1(w, http.StatusOK, struct {
		QueueUrl string `json:"QueueUrl"`
	}{QueueUrl: qu})
}

func (s *Service) jsonListQueues(w http.ResponseWriter, body []byte, region, base, requestID string) {
	var in jListQueues
	_ = json.Unmarshal(body, &in)
	names, err := s.Manager.ListQueues(region, in.QueueNamePrefix)
	if err != nil {
		mapManagerErrJSON(w, err, requestID)
		return
	}
	var urls []string
	for _, n := range names {
		urls = append(urls, s.makeQueueURL(base, region, n))
	}
	w.Header().Set("x-amzn-RequestId", requestID)
	writeJSON1(w, http.StatusOK, struct {
		QueueUrls []string `json:"QueueUrls"`
	}{QueueUrls: urls})
}

func (s *Service) jsonSendMessage(w http.ResponseWriter, body []byte, region, requestID string) {
	var in jSendMessage
	if err := json.Unmarshal(body, &in); err != nil {
		writeJSON1Err(w, http.StatusBadRequest, "com.amazonaws.sqs#MalformedInput", err.Error(), requestID)
		return
	}
	if in.MessageBody == "" {
		writeJSON1Err(w, http.StatusBadRequest, "com.amazonaws.sqs#MissingParameter", "MessageBody is required", requestID)
		return
	}
	if in.QueueUrl == "" {
		writeJSON1Err(w, http.StatusBadRequest, "com.amazonaws.sqs#MissingParameter", "QueueUrl is required", requestID)
		return
	}
	name, err := jsonQueueNameFromURL(in.QueueUrl)
	if err != nil {
		writeJSON1Err(w, http.StatusBadRequest, "com.amazonaws.sqs#InvalidParameter", "invalid QueueUrl", requestID)
		return
	}
	mid, md5b, err := s.Manager.SendMessage(region, name, in.MessageBody)
	if err != nil {
		if errors.Is(err, ErrQueueNotFound) {
			writeJSON1Err(w, http.StatusBadRequest, "com.amazonaws.sqs#QueueDoesNotExist", "The specified queue does not exist.", requestID)
			return
		}
		mapManagerErrJSON(w, err, requestID)
		return
	}
	w.Header().Set("x-amzn-RequestId", requestID)
	writeJSON1(w, http.StatusOK, struct {
		MessageId          string `json:"MessageId"`
		MD5OfMessageBody   string `json:"MD5OfMessageBody"`
	}{MessageId: mid, MD5OfMessageBody: md5b})
}

func (s *Service) jsonReceiveMessage(w http.ResponseWriter, body []byte, region, requestID string) {
	var in jReceiveMessage
	if err := json.Unmarshal(body, &in); err != nil {
		writeJSON1Err(w, http.StatusBadRequest, "com.amazonaws.sqs#MalformedInput", err.Error(), requestID)
		return
	}
	if in.QueueUrl == "" {
		writeJSON1Err(w, http.StatusBadRequest, "com.amazonaws.sqs#MissingParameter", "QueueUrl is required", requestID)
		return
	}
	name, err := jsonQueueNameFromURL(in.QueueUrl)
	if err != nil {
		writeJSON1Err(w, http.StatusBadRequest, "com.amazonaws.sqs#InvalidParameter", "invalid QueueUrl", requestID)
		return
	}
	maxN := 1
	if in.MaxNumberOfMessages != nil && *in.MaxNumberOfMessages > 0 {
		maxN = *in.MaxNumberOfMessages
	}
	waitSec := 0
	if in.WaitTimeSeconds != nil && *in.WaitTimeSeconds > 0 {
		waitSec = *in.WaitTimeSeconds
		if waitSec > 20 {
			waitSec = 20
		}
	}
	deadline := time.Now().Add(time.Duration(waitSec) * time.Second)
	visPtr := in.VisibilityTimeout

	for {
		msgs, err := s.Manager.ReceiveMessage(region, name, maxN, visPtr)
		if err != nil {
			if errors.Is(err, ErrQueueNotFound) {
				writeJSON1Err(w, http.StatusBadRequest, "com.amazonaws.sqs#QueueDoesNotExist", "The specified queue does not exist.", requestID)
				return
			}
			mapManagerErrJSON(w, err, requestID)
			return
		}
		if len(msgs) > 0 {
			var out []jMessageOut
			for _, m := range msgs {
				out = append(out, jMessageOut{
					MessageId:     m.MessageID,
					ReceiptHandle: m.ReceiptHandle,
					MD5OfBody:     m.MD5OfBody,
					Body:          m.Body,
				})
			}
			w.Header().Set("x-amzn-RequestId", requestID)
			writeJSON1(w, http.StatusOK, struct {
				Messages []jMessageOut `json:"Messages"`
			}{Messages: out})
			return
		}
		if waitSec == 0 || time.Now().After(deadline) {
			w.Header().Set("x-amzn-RequestId", requestID)
			writeJSON1(w, http.StatusOK, struct {
				Messages []jMessageOut `json:"Messages"`
			}{Messages: nil})
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (s *Service) jsonDeleteMessage(w http.ResponseWriter, body []byte, region, requestID string) {
	var in jDeleteMessage
	if err := json.Unmarshal(body, &in); err != nil {
		writeJSON1Err(w, http.StatusBadRequest, "com.amazonaws.sqs#MalformedInput", err.Error(), requestID)
		return
	}
	if in.QueueUrl == "" {
		writeJSON1Err(w, http.StatusBadRequest, "com.amazonaws.sqs#MissingParameter", "QueueUrl is required", requestID)
		return
	}
	if in.ReceiptHandle == "" {
		writeJSON1Err(w, http.StatusBadRequest, "com.amazonaws.sqs#MissingParameter", "ReceiptHandle is required", requestID)
		return
	}
	name, err := jsonQueueNameFromURL(in.QueueUrl)
	if err != nil {
		writeJSON1Err(w, http.StatusBadRequest, "com.amazonaws.sqs#InvalidParameter", "invalid QueueUrl", requestID)
		return
	}
	if err := s.Manager.DeleteMessage(region, name); err != nil {
		if errors.Is(err, ErrQueueNotFound) {
			writeJSON1Err(w, http.StatusBadRequest, "com.amazonaws.sqs#QueueDoesNotExist", "The specified queue does not exist.", requestID)
			return
		}
		mapManagerErrJSON(w, err, requestID)
		return
	}
	w.Header().Set("x-amzn-RequestId", requestID)
	writeJSON1(w, http.StatusOK, struct{}{})
}

func (s *Service) jsonPurgeQueue(w http.ResponseWriter, body []byte, region, requestID string) {
	var in jPurgeQueue
	if err := json.Unmarshal(body, &in); err != nil {
		writeJSON1Err(w, http.StatusBadRequest, "com.amazonaws.sqs#MalformedInput", err.Error(), requestID)
		return
	}
	if in.QueueUrl == "" {
		writeJSON1Err(w, http.StatusBadRequest, "com.amazonaws.sqs#MissingParameter", "QueueUrl is required", requestID)
		return
	}
	name, err := jsonQueueNameFromURL(in.QueueUrl)
	if err != nil {
		writeJSON1Err(w, http.StatusBadRequest, "com.amazonaws.sqs#InvalidParameter", "invalid QueueUrl", requestID)
		return
	}
	if err := s.Manager.PurgeQueue(region, name); err != nil {
		if errors.Is(err, ErrQueueNotFound) {
			writeJSON1Err(w, http.StatusBadRequest, "com.amazonaws.sqs#QueueDoesNotExist", "The specified queue does not exist.", requestID)
			return
		}
		mapManagerErrJSON(w, err, requestID)
		return
	}
	w.Header().Set("x-amzn-RequestId", requestID)
	writeJSON1(w, http.StatusOK, struct{}{})
}

func jsonQueueNameFromURL(queueURL string) (string, error) {
	u, err := url.Parse(queueURL)
	if err != nil {
		return "", err
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segs) < 1 {
		return "", errors.New("empty queue path")
	}
	name := segs[len(segs)-1]
	if un, e := url.PathUnescape(name); e == nil {
		name = un
	}
	if !IsValidQueueName(name) {
		return "", errors.New("invalid queue name in QueueUrl")
	}
	return name, nil
}

func mapManagerErrJSON(w http.ResponseWriter, err error, requestID string) {
	switch {
	case errors.Is(err, s3buckets.ErrInvalidRegion), errors.Is(err, ErrInvalidQueueName):
		writeJSON1Err(w, http.StatusBadRequest, "com.amazonaws.sqs#InvalidParameter", err.Error(), requestID)
	default:
		writeJSON1Err(w, http.StatusInternalServerError, "com.amazonaws.sqs#ServiceFailure", err.Error(), requestID)
	}
}

func writeJSON1(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", contentTypeAmzJSON1+"; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

type json1Err struct {
	Type      string `json:"__type"`
	Message   string `json:"message"`
	RequestId string `json:"RequestId,omitempty"`
}

func writeJSON1Err(w http.ResponseWriter, status int, typeURI, message, requestID string) {
	// x-amzn-ErrorType is used by some clients; set alongside JSON body
	w.Header().Set("x-amzn-RequestId", requestID)
	if typeURI != "" {
		// e.g. com.amazonaws.sqs#InvalidAction
		w.Header().Set("x-amzn-ErrorType", typeURI)
	}
	w.Header().Set("Content-Type", contentTypeAmzJSON1+"; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(json1Err{Type: typeURI, Message: message, RequestId: requestID})
}
