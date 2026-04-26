package sqs

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/christian/twister/internal/s3buckets"
)

// Manager stores each queue as one JSON file: Root/{region}/{queueName}.json
type Manager struct {
	Root string
	mu   sync.Mutex
	// DequeueHook, if set, is called (synchronously) after messages are received and
	// removed from the queue (i.e. not when peeking with VisibilityTimeout==0). Used for
	// Lambda event source style triggers.
	DequeueHook func(region, queueName string, msgs []Message)
}

// NewManager returns a Manager. Root should be the cleaned base path (e.g. data/sqs).
func NewManager(root string) *Manager {
	return &Manager{Root: filepath.Clean(root)}
}

var (
	// ErrQueueNotFound is returned when the queue file does not exist.
	ErrQueueNotFound = errors.New("sqs: queue does not exist")
	// ErrInvalidQueueName is returned when the queue name is not allowed.
	ErrInvalidQueueName = errors.New("sqs: invalid queue name")
)

type queueFile struct {
	Messages []storedMsg `json:"messages"`
}

type storedMsg struct {
	ID   string `json:"id"`
	Body string `json:"body"`
}

func (m *Manager) regionDir(region string) string {
	r := s3buckets.NormalizeRegion(region)
	return filepath.Join(m.Root, r)
}

func (m *Manager) queuePath(region, queueName string) (string, error) {
	if !s3buckets.IsValidRegionSegment(s3buckets.NormalizeRegion(region)) {
		return "", s3buckets.ErrInvalidRegion
	}
	if !IsValidQueueName(queueName) {
		return "", ErrInvalidQueueName
	}
	if m == nil || m.Root == "" {
		return "", errors.New("sqs: empty root")
	}
	return filepath.Join(m.regionDir(region), queueName+".json"), nil
}

// CreateQueue creates an empty queue file (idempotent if already exists).
func (m *Manager) CreateQueue(region, queueName string) error {
	p, err := m.queuePath(region, queueName)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	if _, err := os.Stat(p); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return atomicWrite(p, `{"messages":[]}`)
}

// Message is one message returned to ReceiveMessage.
type Message struct {
	MessageID      string
	Body           string
	ReceiptHandle  string
	MD5OfBody      string
}

// SendMessage appends a message; returns the generated message id.
func (m *Manager) SendMessage(region, queueName, body string) (messageID string, md5body string, err error) {
	p, err := m.queuePath(region, queueName)
	if err != nil {
		return "", "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	qf, err := m.readFileLocked(p)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", ErrQueueNotFound
		}
		return "", "", err
	}
	id := randomHex(16)
	qf.Messages = append(qf.Messages, storedMsg{ID: id, Body: body})
	if err := m.writeFileLocked(p, qf); err != nil {
		return "", "", err
	}
	sum := md5.Sum([]byte(body))
	return id, hex.EncodeToString(sum[:]), nil
}

// ReceiveMessage returns up to n messages (FIFO, front of slice).
// visibilityTimeout: if non-nil and *visibilityTimeout == 0, messages are
// read without being removed (peek, matches aws sqs receive-message
// --visibility-timeout 0 on Twister). If nil or any other value, messages
// are removed from the queue (normal receive).
func (m *Manager) ReceiveMessage(region, queueName string, n int, visibilityTimeout *int) ([]Message, error) {
	if n < 1 {
		n = 1
	}
	if n > 10 {
		n = 10
	}
	peek := visibilityTimeout != nil && *visibilityTimeout == 0
	p, err := m.queuePath(region, queueName)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	qf, err := m.readFileLocked(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrQueueNotFound
		}
		return nil, err
	}
	if len(qf.Messages) == 0 {
		return nil, nil
	}
	if len(qf.Messages) < n {
		n = len(qf.Messages)
	}
	take := qf.Messages[:n]
	out := make([]Message, 0, n)
	for _, sm := range take {
		rh := randomHex(32)
		sum := md5.Sum([]byte(sm.Body))
		out = append(out, Message{
			MessageID:     sm.ID,
			Body:          sm.Body,
			ReceiptHandle: rh,
			MD5OfBody:     hex.EncodeToString(sum[:]),
		})
	}
	if peek {
		return out, nil
	}
	qf.Messages = qf.Messages[n:]
	if err := m.writeFileLocked(p, qf); err != nil {
		return nil, err
	}
	if m.DequeueHook != nil && len(out) > 0 {
		// region, queue: recover from path — we have region, queueName in closure via func args
		m.DequeueHook(region, queueName, out)
	}
	return out, nil
}

// PurgeQueue removes all messages from the queue file.
func (m *Manager) PurgeQueue(region, queueName string) error {
	p, err := m.queuePath(region, queueName)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := os.Stat(p); err != nil {
		if os.IsNotExist(err) {
			return ErrQueueNotFound
		}
		return err
	}
	return atomicWrite(p, `{"messages":[]}`)
}

// DeleteMessage succeeds if the queue exists (message may already be consumed by a non-peek ReceiveMessage).
func (m *Manager) DeleteMessage(region, queueName string) error {
	p, err := m.queuePath(region, queueName)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := os.Stat(p); err != nil {
		if os.IsNotExist(err) {
			return ErrQueueNotFound
		}
		return err
	}
	return nil
}

// ListQueues returns queue **names** (not URLs) in the region, optionally filtered by name prefix.
func (m *Manager) ListQueues(region, namePrefix string) ([]string, error) {
	if !s3buckets.IsValidRegionSegment(s3buckets.NormalizeRegion(region)) {
		return nil, s3buckets.ErrInvalidRegion
	}
	if m == nil || m.Root == "" {
		return nil, errors.New("sqs: empty root")
	}
	dir := m.regionDir(region)
	m.mu.Lock()
	defer m.mu.Unlock()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		qn := name[:len(name)-5]
		if !IsValidQueueName(qn) {
			continue
		}
		if namePrefix != "" && !strings.HasPrefix(qn, namePrefix) {
			continue
		}
		names = append(names, qn)
	}
	return names, nil
}

// QueueHas returns whether the queue file exists.
func (m *Manager) QueueHas(region, queueName string) (bool, error) {
	p, err := m.queuePath(region, queueName)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(p)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (m *Manager) readFileLocked(path string) (*queueFile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var qf queueFile
	if len(b) == 0 {
		return &queueFile{Messages: nil}, nil
	}
	if err := json.Unmarshal(b, &qf); err != nil {
		return nil, err
	}
	return &qf, nil
}

func (m *Manager) writeFileLocked(path string, qf *queueFile) error {
	b, err := json.MarshalIndent(qf, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteBytes(path, append(b, '\n'))
}

func atomicWrite(path string, data string) error {
	return atomicWriteBytes(path, []byte(data))
}

func atomicWriteBytes(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func randomHex(nBytes int) string {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return strings.Repeat("0", nBytes*2)
	}
	return hex.EncodeToString(b)
}
