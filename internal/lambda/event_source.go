package lambda

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/christian/twister/internal/s3buckets"
)

// EventSourceMapping is a row compatible with a subset of AWS event source mapping.
type EventSourceMapping struct {
	UUID             string `json:"UUID"`
	EventSourceArn   string `json:"EventSourceArn"`
	FunctionName     string `json:"FunctionName"`
	State            string `json:"State"`
	BatchSize        int    `json:"BatchSize,omitempty"`
}

// EventSourceStore holds SQS -> Lambda event source mappings (v1, single file).
type EventSourceStore struct {
	Path string
	mu   sync.Mutex
}

// NewEventSourceStore uses {registryRoot}/event-source-mappings.json.
func NewEventSourceStore(registryRoot string) *EventSourceStore {
	return &EventSourceStore{Path: filepath.Join(filepath.Clean(registryRoot), "event-source-mappings.json")}
}

type mappingFile struct {
	Mappings []EventSourceMapping `json:"Mappings"`
}

func (s *EventSourceStore) load() (mappingFile, error) {
	var m mappingFile
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return m, nil
		}
		return m, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, err
	}
	return m, nil
}

// FindFunctionForSQS returns the function name if an enabled mapping exists for
// the SQS queue in the given region, else "".
func (s *EventSourceStore) FindFunctionForSQS(region, queueName string) (string, error) {
	m, err := s.load()
	if err != nil {
		return "", err
	}
	region = s3buckets.NormalizeRegion(region)
	wantARN := "arn:aws:sqs:" + region + ":"
	for _, row := range m.Mappings {
		if strings.ToLower(row.State) == "disabled" {
			continue
		}
		if !strings.HasPrefix(row.EventSourceArn, wantARN) {
			continue
		}
		// arn:...:queueName
		arn := row.EventSourceArn
		if last := arnLastSegment(arn); last == queueName {
			return row.FunctionName, nil
		}
	}
	return "", nil
}

func arnLastSegment(arn string) string {
	arn = strings.TrimSpace(arn)
	i := strings.LastIndex(arn, ":")
	if i < 0 {
		return arn
	}
	return arn[i+1:]
}

// AddEventSourceMapping appends a mapping; UUID must be set by caller.
func (s *EventSourceStore) AddEventSourceMapping(m EventSourceMapping) error {
	if m.FunctionName == "" || m.EventSourceArn == "" {
		return errors.New("lambda: EventSourceArn and FunctionName are required")
	}
	if m.State == "" {
		m.State = "Enabled"
	}
	if m.BatchSize == 0 {
		m.BatchSize = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.loadLocked()
	if err != nil {
		return err
	}
	for _, e := range data.Mappings {
		if e.EventSourceArn == m.EventSourceArn {
			return errors.New("lambda: an event source mapping for this queue already exists")
		}
	}
	data.Mappings = append(data.Mappings, m)
	return s.saveLocked(data)
}

func (s *EventSourceStore) loadLocked() (mappingFile, error) {
	var m mappingFile
	b, err := os.ReadFile(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return m, nil
		}
		return m, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, err
	}
	return m, nil
}

func (s *EventSourceStore) saveLocked(m mappingFile) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o750); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.Path)
}

// DeleteByUUID removes a mapping by UUID.
func (s *EventSourceStore) DeleteByUUID(uuid string) error {
	if uuid == "" {
		return errors.New("lambda: empty UUID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.loadLocked()
	if err != nil {
		return err
	}
	var out []EventSourceMapping
	for _, e := range data.Mappings {
		if e.UUID != uuid {
			out = append(out, e)
		}
	}
	if len(out) == len(data.Mappings) {
		return errors.New("lambda: no such event source mapping")
	}
	data.Mappings = out
	return s.saveLocked(data)
}

// ListEventSourceMappings returns all mappings.
func (s *EventSourceStore) ListEventSourceMappings() ([]EventSourceMapping, error) {
	m, err := s.load()
	if err != nil {
		return nil, err
	}
	return m.Mappings, nil
}
