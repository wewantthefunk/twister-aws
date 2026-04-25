package paramstore

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"time"
)

// DefaultRegion matches AWS CLI defaults and secretstore; used when a row omits region.
const DefaultRegion = "us-east-1"

// NormalizeRegion lowercases and trims; empty means DefaultRegion.
func NormalizeRegion(s string) string {
	t := strings.ToLower(strings.TrimSpace(s))
	if t == "" {
		return DefaultRegion
	}
	return t
}

type fileEntry struct {
	Name         string          `json:"name"`
	Region       string          `json:"region,omitempty"`
	Value        string          `json:"value"`
	Type         string          `json:"type,omitempty"`
	Version      int             `json:"version"`
	LastModified json.RawMessage `json:"lastModified,omitempty"`
}

// ParameterRecord is one SSM parameter (PutParameter / GetParameter).
type ParameterRecord struct {
	Region       string
	Name         string
	Type         string
	Value        string
	Version      int
	LastModified time.Time
}

// Store holds mock parameters by (region, name).
type Store struct {
	mu    sync.Mutex
	byKey map[string]*ParameterRecord
}

// NewStore returns an empty store.
func NewStore() *Store {
	return &Store{byKey: make(map[string]*ParameterRecord)}
}

func regionNameKey(region, name string) string {
	return NormalizeRegion(region) + "\x00" + name
}

func (s *Store) putLocked(rec *ParameterRecord) {
	if rec == nil || strings.TrimSpace(rec.Name) == "" {
		return
	}
	cp := *rec
	cp.Region = NormalizeRegion(cp.Region)
	if cp.Type == "" {
		cp.Type = "String"
	}
	if cp.Version < 1 {
		cp.Version = 1
	}
	if cp.LastModified.IsZero() {
		cp.LastModified = time.Now().UTC()
	}
	if s.byKey == nil {
		s.byKey = make(map[string]*ParameterRecord)
	}
	s.byKey[regionNameKey(cp.Region, cp.Name)] = &cp
}

// Put replaces or adds a parameter (concurrent-safe).
func (s *Store) Put(rec *ParameterRecord) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.putLocked(rec)
}

// Get returns a copy of the (region, name) record, or nil.
func (s *Store) getLocked(region, name string) *ParameterRecord {
	if s == nil {
		return nil
	}
	r := s.byKey[regionNameKey(region, name)]
	if r == nil {
		return nil
	}
	cp := *r
	return &cp
}

// NameAndRegionFromID parses a plain parameter name or an
// arn:aws:ssm:region:account:parameter/... ARN. The returned
// arnRegion is set only for valid SSM ARNs; parameter name is
// normalized to match how we key the store: leading slash in path
// form when the resource path is hierarchical.
func NameAndRegionFromID(id string) (name string, arnRegion string) {
	id = strings.TrimSpace(id)
	if !strings.HasPrefix(id, "arn:") {
		return id, ""
	}
	parts := strings.SplitN(id, ":", 6)
	if len(parts) < 6 || parts[1] != "aws" || parts[2] != "ssm" {
		return id, ""
	}
	arnRegion = parts[3]
	res := parts[5] // e.g. parameter/Prod/foo or parameter/MyName
	if !strings.HasPrefix(res, "parameter/") {
		return id, arnRegion
	}
	tail := strings.TrimPrefix(res, "parameter/")
	// Hierarchical names are stored with a leading /.
	if tail != "" && !strings.HasPrefix(tail, "/") {
		name = "/" + tail
	} else {
		name = tail
	}
	if name == "" {
		return id, arnRegion
	}
	return name, arnRegion
}

// LookupInRegion resolves Name or SSM parameter ARN, enforcing request
// region against the key (and, for ARNs, the ARN region as well).
func (s *Store) LookupInRegion(nameOrARN, requestRegion string) *ParameterRecord {
	if s == nil {
		return nil
	}
	name, arnR := NameAndRegionFromID(nameOrARN)
	if name == "" {
		return nil
	}
	if arnR != "" && !strings.EqualFold(NormalizeRegion(arnR), NormalizeRegion(requestRegion)) {
		return nil
	}
	rr := NormalizeRegion(requestRegion)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getLocked(rr, name)
}

// Count returns the number of parameters in the store.
func (s *Store) Count() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.byKey)
}

// LoadParametersJSON loads a JSON array into the store (replaces
// (region, name) entries when the same name appears again).
func LoadParametersJSON(path string, store *Store) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var entries []fileEntry
	if err := json.Unmarshal(b, &entries); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, e := range entries {
		if e.Name == "" {
			continue
		}
		t := e.Type
		if t == "" {
			t = "String"
		}
		v := e.Version
		if v < 1 {
			v = 1
		}
		var mod time.Time
		if len(e.LastModified) > 0 {
			var f float64
			if err := json.Unmarshal(e.LastModified, &f); err == nil {
				sec, frac := math.Modf(f)
				mod = time.Unix(int64(sec), int64(frac*1e9))
			} else {
				var tstr string
				if err := json.Unmarshal(e.LastModified, &tstr); err == nil {
					mod, _ = time.Parse(time.RFC3339, tstr)
				}
			}
		}
		if mod.IsZero() {
			mod = time.Now().UTC()
		}
		store.putLocked(&ParameterRecord{
			Region:       NormalizeRegion(e.Region),
			Name:         e.Name,
			Type:         t,
			Value:        e.Value,
			Version:      v,
			LastModified: mod,
		})
	}
	return nil
}

// SynthesizeParameterARN returns a mock ARN (account 000000000000) for responses.
func SynthesizeParameterARN(region, name string) string {
	r := NormalizeRegion(region)
	// Path-style names: resource part is "parameter" + name without a leading "/".
	if strings.HasPrefix(name, "/") {
		return fmt.Sprintf("arn:aws:ssm:%s:000000000000:parameter%s", r, name)
	}
	return fmt.Sprintf("arn:aws:ssm:%s:000000000000:parameter/%s", r, name)
}

// LastModifiedFloat returns a Unix time as float (AWS JSON).
func LastModifiedFloat(t time.Time) float64 {
	return float64(t.Unix()) + float64(t.Nanosecond())/1e9
}
