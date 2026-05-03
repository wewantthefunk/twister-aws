package sqs

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/christian/twister/internal/s3buckets"
)

const (
	sqsXMLNS        = "http://queue.amazonaws.com/doc/2012-11-05/"
	sqsQueryVersion = "2012-11-05"
	placeholderAcct = "000000000000"
)

// Service implements the SQS Query API (form POST) and the AWS JSON 1.0 API
// (application/x-amz-json-1.0 + X-Amz-Target), which is the default for the AWS CLI v2.
type Service struct {
	*Manager
}

// NewService returns a Service that persists under root (e.g. data/sqs).
func NewService(root string) *Service {
	return &Service{Manager: NewManager(root)}
}

// Handle runs after successful SigV4 with credential scope "sqs".
func (s *Service) Handle(w http.ResponseWriter, r *http.Request, region string, body []byte, requestID string) {
	if s == nil || s.Manager == nil {
		writeXMLErr(w, http.StatusInternalServerError, "ServiceFailure", "sqs not configured", requestID)
		return
	}
	ct := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	if isAmzJSON1(ct) {
		s.handleAmzJSON(w, r, body, requestID, region)
		return
	}
	if !strings.HasPrefix(ct, "application/x-www-form-urlencoded") {
		writeXMLErr(w, http.StatusBadRequest, "InvalidParameterValue", "Content-Type must be application/x-amz-json-1.0 (AWS CLI default) or application/x-www-form-urlencoded", requestID)
		return
	}
	q, err := url.ParseQuery(string(body))
	if err != nil {
		writeXMLErr(w, http.StatusBadRequest, "MalformedQueryString", "invalid form body", requestID)
		return
	}
	if v := q.Get("Version"); v != "" && v != sqsQueryVersion {
		writeXMLErr(w, http.StatusBadRequest, "InvalidParameter", fmt.Sprintf("unsupported Version %q (expected %s)", v, sqsQueryVersion), requestID)
		return
	}

	region = s3buckets.NormalizeRegion(region)
	if !s3buckets.IsValidRegionSegment(region) {
		writeXMLErr(w, http.StatusBadRequest, "InvalidParameter", "invalid signing region in credential scope", requestID)
		return
	}

	base := requestBaseURL(r)
	action := strings.TrimSpace(q.Get("Action"))
	switch action {
	case "CreateQueue":
		s.createQueue(w, r, q, region, base, requestID)
	case "GetQueueUrl":
		s.getQueueURL(w, q, region, base, requestID)
	case "ListQueues":
		s.listQueues(w, q, region, base, requestID)
	case "SendMessage":
		s.sendMessage(w, q, region, requestID)
	case "ReceiveMessage":
		s.receiveMessage(w, q, region, requestID)
	case "DeleteMessage":
		s.deleteMessage(w, q, region, requestID)
	case "PurgeQueue":
		s.purgeQueue(w, q, region, requestID)
	case "":
		writeXMLErr(w, http.StatusBadRequest, "InvalidParameter", "Action is required", requestID)
	default:
		writeXMLErr(w, http.StatusBadRequest, "InvalidAction", "unsupported Action "+action, requestID)
	}
}

func requestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if x := r.Header.Get("X-Forwarded-Proto"); x == "https" || x == "http" {
		scheme = x
	}
	return scheme + "://" + r.Host
}

func (s *Service) makeQueueURL(base, _ /*region*/ string, queueName string) string {
	return base + "/" + placeholderAcct + "/" + url.PathEscape(queueName)
}

func parseQueueNameFromQueueURL(queueURL string) (string, error) {
	u, err := url.Parse(queueURL)
	if err != nil {
		return "", err
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segs) < 1 {
		return "", errors.New("empty queue path")
	}
	return segs[len(segs)-1], nil
}

func (s *Service) createQueue(w http.ResponseWriter, r *http.Request, q url.Values, region, base, requestID string) {
	_ = r
	name := q.Get("QueueName")
	if name == "" {
		writeXMLErr(w, http.StatusBadRequest, "InvalidParameter", "QueueName is required", requestID)
		return
	}
	if !IsValidQueueName(name) {
		writeXMLErr(w, http.StatusBadRequest, "InvalidParameter", "Invalid queue name", requestID)
		return
	}
	if err := s.Manager.CreateQueue(region, name); err != nil {
		mapManagerErr(w, err, requestID)
		return
	}
	qu := s.makeQueueURL(base, region, name)
	s.writeCreateQueue(w, qu, requestID)
}

func (s *Service) getQueueURL(w http.ResponseWriter, q url.Values, region, base, requestID string) {
	name := q.Get("QueueName")
	if name == "" {
		writeXMLErr(w, http.StatusBadRequest, "InvalidParameter", "QueueName is required", requestID)
		return
	}
	ok, err := s.Manager.QueueHas(region, name)
	if err != nil {
		mapManagerErr(w, err, requestID)
		return
	}
	if !ok {
		writeXMLErr(w, http.StatusBadRequest, "AWS.SimpleQueueService.NonExistentQueue", "The specified queue does not exist.", requestID)
		return
	}
	qu := s.makeQueueURL(base, region, name)
	s.writeGetQueueURL(w, qu, requestID)
}

func (s *Service) listQueues(w http.ResponseWriter, q url.Values, region, base, requestID string) {
	prefix := q.Get("QueueNamePrefix")
	names, err := s.Manager.ListQueues(region, prefix)
	if err != nil {
		mapManagerErr(w, err, requestID)
		return
	}
	var urls []string
	for _, n := range names {
		urls = append(urls, s.makeQueueURL(base, region, n))
	}
	s.writeListQueues(w, urls, requestID)
}

func (s *Service) sendMessage(w http.ResponseWriter, q url.Values, region, requestID string) {
	body := q.Get("MessageBody")
	if body == "" {
		writeXMLErr(w, http.StatusBadRequest, "InvalidParameter", "MessageBody is required", requestID)
		return
	}
	qu := q.Get("QueueUrl")
	if qu == "" {
		writeXMLErr(w, http.StatusBadRequest, "InvalidParameter", "QueueUrl is required", requestID)
		return
	}
	name, err := parseQueueNameFromQueueURL(qu)
	if err != nil {
		writeXMLErr(w, http.StatusBadRequest, "InvalidParameter", "invalid QueueUrl", requestID)
		return
	}
	if unesc, e := url.PathUnescape(name); e == nil {
		name = unesc
	}
	if !IsValidQueueName(name) {
		writeXMLErr(w, http.StatusBadRequest, "InvalidParameter", "invalid queue name in QueueUrl", requestID)
		return
	}
	mid, md5b, err := s.Manager.SendMessage(region, name, body)
	if err != nil {
		if errors.Is(err, ErrQueueNotFound) {
			writeXMLErr(w, http.StatusBadRequest, "AWS.SimpleQueueService.NonExistentQueue", "The specified queue does not exist.", requestID)
			return
		}
		mapManagerErr(w, err, requestID)
		return
	}
	s.writeSendMessage(w, mid, md5b, requestID)
}

func (s *Service) receiveMessage(w http.ResponseWriter, q url.Values, region, requestID string) {
	qu := q.Get("QueueUrl")
	if qu == "" {
		writeXMLErr(w, http.StatusBadRequest, "InvalidParameter", "QueueUrl is required", requestID)
		return
	}
	name, err := parseQueueNameFromQueueURL(qu)
	if err != nil {
		writeXMLErr(w, http.StatusBadRequest, "InvalidParameter", "invalid QueueUrl", requestID)
		return
	}
	if unesc, e := url.PathUnescape(name); e == nil {
		name = unesc
	}
	if !IsValidQueueName(name) {
		writeXMLErr(w, http.StatusBadRequest, "InvalidParameter", "invalid queue name in QueueUrl", requestID)
		return
	}
	maxN := 1
	if s := q.Get("MaxNumberOfMessages"); s != "" {
		if n, e := strconv.Atoi(s); e == nil && n > 0 {
			maxN = n
		}
	}
	var visPtr *int
	if q.Has("VisibilityTimeout") {
		if s := q.Get("VisibilityTimeout"); s != "" {
			if n, e := strconv.Atoi(s); e == nil {
				v := n
				visPtr = &v
			}
		} else {
			zero := 0
			visPtr = &zero
		}
	}
	waitSec := 0
	if s := q.Get("WaitTimeSeconds"); s != "" {
		if n, e := strconv.Atoi(s); e == nil && n > 0 {
			waitSec = n
			if waitSec > 20 {
				waitSec = 20
			}
		}
	}

	deadline := time.Now().Add(time.Duration(waitSec) * time.Second)
	for {
		msgs, err := s.Manager.ReceiveMessage(region, name, maxN, visPtr)
		if err != nil {
			if errors.Is(err, ErrQueueNotFound) {
				writeXMLErr(w, http.StatusBadRequest, "AWS.SimpleQueueService.NonExistentQueue", "The specified queue does not exist.", requestID)
				return
			}
			mapManagerErr(w, err, requestID)
			return
		}
		if len(msgs) > 0 {
			s.writeReceiveMessage(w, msgs, requestID)
			return
		}
		if waitSec == 0 || time.Now().After(deadline) {
			s.writeReceiveMessage(w, nil, requestID)
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (s *Service) deleteMessage(w http.ResponseWriter, q url.Values, region, requestID string) {
	qu := q.Get("QueueUrl")
	if qu == "" {
		writeXMLErr(w, http.StatusBadRequest, "InvalidParameter", "QueueUrl is required", requestID)
		return
	}
	if q.Get("ReceiptHandle") == "" {
		writeXMLErr(w, http.StatusBadRequest, "InvalidParameter", "ReceiptHandle is required", requestID)
		return
	}
	name, err := parseQueueNameFromQueueURL(qu)
	if err != nil {
		writeXMLErr(w, http.StatusBadRequest, "InvalidParameter", "invalid QueueUrl", requestID)
		return
	}
	if unesc, e := url.PathUnescape(name); e == nil {
		name = unesc
	}
	if err := s.Manager.DeleteMessage(region, name); err != nil {
		if errors.Is(err, ErrQueueNotFound) {
			writeXMLErr(w, http.StatusBadRequest, "AWS.SimpleQueueService.NonExistentQueue", "The specified queue does not exist.", requestID)
			return
		}
		mapManagerErr(w, err, requestID)
		return
	}
	s.writeDeleteMessage(w, requestID)
}

func (s *Service) purgeQueue(w http.ResponseWriter, q url.Values, region, requestID string) {
	qu := q.Get("QueueUrl")
	if qu == "" {
		writeXMLErr(w, http.StatusBadRequest, "InvalidParameter", "QueueUrl is required", requestID)
		return
	}
	name, err := parseQueueNameFromQueueURL(qu)
	if err != nil {
		writeXMLErr(w, http.StatusBadRequest, "InvalidParameter", "invalid QueueUrl", requestID)
		return
	}
	if unesc, e := url.PathUnescape(name); e == nil {
		name = unesc
	}
	if err := s.Manager.PurgeQueue(region, name); err != nil {
		if errors.Is(err, ErrQueueNotFound) {
			writeXMLErr(w, http.StatusBadRequest, "AWS.SimpleQueueService.NonExistentQueue", "The specified queue does not exist.", requestID)
			return
		}
		mapManagerErr(w, err, requestID)
		return
	}
	s.writePurgeQueue(w, requestID)
}

func mapManagerErr(w http.ResponseWriter, err error, requestID string) {
	switch {
	case errors.Is(err, s3buckets.ErrInvalidRegion):
		writeXMLErr(w, http.StatusBadRequest, "InvalidParameter", err.Error(), requestID)
	case errors.Is(err, ErrInvalidQueueName):
		writeXMLErr(w, http.StatusBadRequest, "InvalidParameter", err.Error(), requestID)
	default:
		writeXMLErr(w, http.StatusInternalServerError, "ServiceFailure", err.Error(), requestID)
	}
}

// --- XML response helpers ---

type responseMetadata struct {
	RequestId string `xml:"RequestId"`
}

func (s *Service) writeCreateQueue(w http.ResponseWriter, queueURL, requestID string) {
	t := createQueueResponse{
		XMLName:           xml.Name{Local: "CreateQueueResponse"},
		Xmlns:             sqsXMLNS,
		CreateQueueResult: queueURLInner{QueueUrl: queueURL},
		ResponseMetadata:  responseMetadata{RequestId: requestID},
	}
	writeXMLResponse(w, &t)
}

type queueURLInner struct {
	QueueUrl string `xml:"QueueUrl"`
}

type createQueueResponse struct {
	XMLName           xml.Name         `xml:"CreateQueueResponse"`
	Xmlns             string           `xml:"xmlns,attr"`
	CreateQueueResult queueURLInner    `xml:"CreateQueueResult"`
	ResponseMetadata  responseMetadata `xml:"ResponseMetadata"`
}

func (s *Service) writeGetQueueURL(w http.ResponseWriter, queueURL, requestID string) {
	t := getQueueUrlResponse{
		XMLName:           xml.Name{Local: "GetQueueUrlResponse"},
		Xmlns:             sqsXMLNS,
		GetQueueUrlResult: queueURLInner{QueueUrl: queueURL},
		ResponseMetadata:  responseMetadata{RequestId: requestID},
	}
	writeXMLResponse(w, &t)
}

type getQueueUrlResponse struct {
	XMLName           xml.Name         `xml:"GetQueueUrlResponse"`
	Xmlns             string           `xml:"xmlns,attr"`
	GetQueueUrlResult queueURLInner    `xml:"GetQueueUrlResult"`
	ResponseMetadata  responseMetadata `xml:"ResponseMetadata"`
}

type listQueuesResponse struct {
	XMLName         xml.Name          `xml:"ListQueuesResponse"`
	Xmlns           string            `xml:"xmlns,attr"`
	ListQueuesResult listQueuesResult `xml:"ListQueuesResult"`
	ResponseMetadata responseMetadata `xml:"ResponseMetadata"`
}

type listQueuesResult struct {
	QueueUrl []string `xml:"QueueUrl"`
}

func (s *Service) writeListQueues(w http.ResponseWriter, urls []string, requestID string) {
	t := listQueuesResponse{
		XMLName:         xml.Name{Local: "ListQueuesResponse"},
		Xmlns:           sqsXMLNS,
		ListQueuesResult: listQueuesResult{QueueUrl: urls},
		ResponseMetadata: responseMetadata{RequestId: requestID},
	}
	writeXMLResponse(w, &t)
}

type sendMessageResponse struct {
	XMLName          xml.Name         `xml:"SendMessageResponse"`
	Xmlns            string           `xml:"xmlns,attr"`
	SendMessageResult struct {
		MessageId        string `xml:"MessageId"`
		MD5OfMessageBody string `xml:"MD5OfMessageBody"`
	} `xml:"SendMessageResult"`
	ResponseMetadata responseMetadata `xml:"ResponseMetadata"`
}

func (s *Service) writeSendMessage(w http.ResponseWriter, messageID, md5b, requestID string) {
	t := sendMessageResponse{XMLName: xml.Name{Local: "SendMessageResponse"}, Xmlns: sqsXMLNS}
	t.SendMessageResult.MessageId = messageID
	t.SendMessageResult.MD5OfMessageBody = md5b
	t.ResponseMetadata = responseMetadata{RequestId: requestID}
	writeXMLResponse(w, &t)
}

type receiveMessageResponse struct {
	XMLName              xml.Name              `xml:"ReceiveMessageResponse"`
	Xmlns                string                `xml:"xmlns,attr"`
	ReceiveMessageResult *receiveResultInner  `xml:"ReceiveMessageResult,omitempty"`
	ResponseMetadata     responseMetadata     `xml:"ResponseMetadata"`
}

type receiveResultInner struct {
	Messages []rxMsg `xml:"Message"`
}

type rxMsg struct {
	MessageId      string `xml:"MessageId"`
	ReceiptHandle  string `xml:"ReceiptHandle"`
	MD5OfBody      string `xml:"MD5OfBody"`
	Body           string `xml:"Body"`
}

func (s *Service) writeReceiveMessage(w http.ResponseWriter, msgs []Message, requestID string) {
	t := receiveMessageResponse{XMLName: xml.Name{Local: "ReceiveMessageResponse"}, Xmlns: sqsXMLNS, ResponseMetadata: responseMetadata{RequestId: requestID}}
	if len(msgs) > 0 {
		inner := &receiveResultInner{}
		for _, m := range msgs {
			inner.Messages = append(inner.Messages, rxMsg{
				MessageId:     m.MessageID,
				ReceiptHandle: m.ReceiptHandle,
				MD5OfBody:     m.MD5OfBody,
				Body:          m.Body,
			})
		}
		t.ReceiveMessageResult = inner
	} else {
		t.ReceiveMessageResult = &receiveResultInner{Messages: nil}
	}
	writeXMLResponse(w, &t)
}

type deleteMessageResponse struct {
	XMLName          xml.Name         `xml:"DeleteMessageResponse"`
	Xmlns            string           `xml:"xmlns,attr"`
	ResponseMetadata responseMetadata `xml:"ResponseMetadata"`
}

func (s *Service) writeDeleteMessage(w http.ResponseWriter, requestID string) {
	t := deleteMessageResponse{XMLName: xml.Name{Local: "DeleteMessageResponse"}, Xmlns: sqsXMLNS, ResponseMetadata: responseMetadata{RequestId: requestID}}
	writeXMLResponse(w, &t)
}

type purgeQueueResponse struct {
	XMLName          xml.Name         `xml:"PurgeQueueResponse"`
	Xmlns            string           `xml:"xmlns,attr"`
	ResponseMetadata responseMetadata `xml:"ResponseMetadata"`
}

func (s *Service) writePurgeQueue(w http.ResponseWriter, requestID string) {
	t := purgeQueueResponse{XMLName: xml.Name{Local: "PurgeQueueResponse"}, Xmlns: sqsXMLNS, ResponseMetadata: responseMetadata{RequestId: requestID}}
	writeXMLResponse(w, &t)
}

func writeXMLResponse(w http.ResponseWriter, v interface{}) {
	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	enc := xml.NewEncoder(&buf)
	enc.Indent("", "  ")
	if err := enc.Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = enc.Flush()
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

// --- error XML ---

type sqsErrResponse struct {
	XMLName   xml.Name   `xml:"ErrorResponse"`
	Xmlns     string     `xml:"xmlns,attr"`
	Error     sqsErrBody `xml:"Error"`
	RequestId string     `xml:"RequestId"`
}

type sqsErrBody struct {
	Type    string `xml:"Type"`
	Code    string `xml:"Code"`
	Message string `xml:"Message"`
}

func writeXMLErr(w http.ResponseWriter, status int, code, message, requestID string) {
	t := sqsErrResponse{
		XMLName:   xml.Name{Local: "ErrorResponse"},
		Xmlns:     sqsXMLNS,
		Error:     sqsErrBody{Type: "Sender", Code: code, Message: message},
		RequestId: requestID,
	}
	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	enc := xml.NewEncoder(&buf)
	_ = enc.Encode(&t)
	_ = enc.Flush()
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

// WriteNotConfigured responds when the router has no SQS service (503).
func WriteNotConfigured(w http.ResponseWriter, requestID string) {
	writeXMLErr(w, http.StatusServiceUnavailable, "ServiceUnavailable", "SQS is not enabled on this server", requestID)
}
