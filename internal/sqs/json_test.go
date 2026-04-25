package sqs

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsAmzJSON1(t *testing.T) {
	if !isAmzJSON1("application/x-amz-json-1.0") {
		t.Fatal("plain")
	}
	if !isAmzJSON1("application/x-amz-json-1.0; charset=utf-8") {
		t.Fatal("with charset")
	}
	if isAmzJSON1("application/x-www-form-urlencoded") {
		t.Fatal("form should be false")
	}
}

func TestHandleAmzJSON_CreateQueue(t *testing.T) {
	dir := t.TempDir()
	s := NewService(dir)
	body := []byte(`{"QueueName":"first-queue"}`)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-amz-json-1.0")
	req.Header.Set("X-Amz-Target", "AmazonSQS.CreateQueue")
	w := httptest.NewRecorder()
	s.handleAmzJSON(w, req, body, "rid-1", "us-east-1")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct == "" || !isAmzJSON1(ct) {
		t.Fatalf("content-type: %q", ct)
	}
	var out struct {
		QueueUrl string `json:"QueueUrl"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.QueueUrl == "" {
		t.Fatalf("queue url empty")
	}
}

func TestParseAmzTargetOp(t *testing.T) {
	if got := parseAmzTargetOp("AmazonSQS.CreateQueue"); got != "CreateQueue" {
		t.Fatalf("got %q", got)
	}
	if got := parseAmzTargetOp("com.amazonaws.sqs#SendMessage"); got != "SendMessage" {
		t.Fatalf("got %q", got)
	}
}

func TestService_Handle_routesJSON(t *testing.T) {
	dir := t.TempDir()
	s := NewService(dir)
	body := []byte(`{"QueueName":"q"}`)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-amz-json-1.0; charset=utf-8")
	req.Header.Set("X-Amz-Target", "AmazonSQS.CreateQueue")
	w := httptest.NewRecorder()
	s.Handle(w, req, "us-east-1", body, "rid")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
}

// Ensure form-urlencoded path still works.
func TestService_Handle_routesForm(t *testing.T) {
	dir := t.TempDir()
	s := NewService(dir)
	body := []byte("Action=CreateQueue&Version=2012-11-05&QueueName=formq")
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	w := httptest.NewRecorder()
	s.Handle(w, req, "us-east-1", body, "rid")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("CreateQueueResponse")) {
		t.Fatalf("expected XML, got: %s", w.Body.String())
	}
}

func TestRequestBaseURL(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://localhost:8080/", nil)
	if got := requestBaseURL(r); got != "http://localhost:8080" {
		t.Fatalf("got %q", got)
	}
	// drain body
	_, _ = io.ReadAll(r.Body)
}
