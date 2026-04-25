package sqs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManager_createSendReceivePop(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)
	region := "us-east-1"
	name := "my-queue"
	if err := m.CreateQueue(region, name); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, region, name+".json")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("expected queue file: %v", err)
	}
	mid, md5b, err := m.SendMessage(region, name, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if len(mid) < 8 || len(md5b) != 32 {
		t.Fatalf("bad ids: %q %q", mid, md5b)
	}
	msg, err := m.ReceiveMessage(region, name, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(msg) != 1 {
		t.Fatalf("got %d messages", len(msg))
	}
	if msg[0].Body != "hello" || msg[0].MessageID != mid {
		t.Fatalf("msg = %#v", msg[0])
	}
	msg2, err := m.ReceiveMessage(region, name, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(msg2) != 0 {
		t.Fatalf("expected empty queue, got %#v", msg2)
	}
}

func TestListQueues(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)
	_ = m.CreateQueue("us-west-2", "a")
	_ = m.CreateQueue("us-west-2", "b")
	names, err := m.ListQueues("us-west-2", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 {
		t.Fatalf("names: %v", names)
	}
}

func TestManager_peekVisibilityTimeout0(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)
	region, name := "us-east-1", "q"
	_ = m.CreateQueue(region, name)
	_, _, err := m.SendMessage(region, name, "peek-me")
	if err != nil {
		t.Fatal(err)
	}
	z := 0
	a, err := m.ReceiveMessage(region, name, 1, &z)
	if err != nil || len(a) != 1 || a[0].Body != "peek-me" {
		t.Fatalf("peek a: err=%v %#v", err, a)
	}
	b, err := m.ReceiveMessage(region, name, 1, &z)
	if err != nil || len(b) != 1 || b[0].Body != "peek-me" {
		t.Fatalf("peek b: err=%v %#v (message should still be in queue)", err, b)
	}
	_, err = m.ReceiveMessage(region, name, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	c, err := m.ReceiveMessage(region, name, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(c) != 0 {
		t.Fatalf("queue should be empty after pop, got %#v", c)
	}
}
