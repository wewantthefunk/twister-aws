package secretstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"time"
)

// DefaultRegion is used when a secret row omits a region (compatibility) or for seeded demo secrets.
const DefaultRegion = "us-east-1"

type fileEntry struct {
	Name         string          `json:"name"`
	Region       string          `json:"region,omitempty"`
	SecretString string          `json:"secretString"`
	CreatedDate  json.RawMessage `json:"createdDate,omitempty"`
	VersionID    string          `json:"versionId,omitempty"`
}

// SecretRecord is one logical secret returned by GetSecretValue.
type SecretRecord struct {
	Region       string
	Name         string
	SecretString string
	CreatedDate  time.Time
	VersionID    string
}

// Store holds mock secrets keyed by (region, name); same name in two regions is two records.
type Store struct {
	mu     sync.Mutex
	byKey  map[string]*SecretRecord
}

// NewStore returns an empty store.
func NewStore() *Store {
	return &Store{byKey: make(map[string]*SecretRecord)}
}

// NormalizeRegion lowercases and trims; empty becomes DefaultRegion.
func NormalizeRegion(s string) string {
	t := strings.ToLower(strings.TrimSpace(s))
	if t == "" {
		return DefaultRegion
	}
	return t
}

func regionStorageKey(region, name string) string {
	return NormalizeRegion(region) + "\x00" + strings.TrimSpace(name)
}

// putLocked stores a copy of rec (caller may reuse buffers).
func (s *Store) putLocked(rec *SecretRecord) {
	if rec == nil || strings.TrimSpace(rec.Name) == "" {
		return
	}
	cp := *rec
	cp.Region = NormalizeRegion(cp.Region)
	if s.byKey == nil {
		s.byKey = make(map[string]*SecretRecord)
	}
	s.byKey[regionStorageKey(cp.Region, cp.Name)] = &cp
}

// Put inserts or replaces a secret by name.
func (s *Store) Put(rec *SecretRecord) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.putLocked(rec)
}

func parseCreatedDate(raw json.RawMessage) (time.Time, error) {
	if len(raw) == 0 {
		return time.Now().UTC(), nil
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		sec, frac := math.Modf(f)
		return time.Unix(int64(sec), int64(frac*1e9)).UTC(), nil
	}
	var str string
	if err := json.Unmarshal(raw, &str); err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339, str)
}

// LoadSecretsJSON loads secrets from a JSON array file into the store.
func LoadSecretsJSON(path string, store *Store) error {
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
		created, err := parseCreatedDate(e.CreatedDate)
		if err != nil {
			created = time.Now().UTC()
		}
		vid := e.VersionID
		if vid == "" {
			vid = DefaultVersionID(e.Name)
		}
		store.putLocked(&SecretRecord{
			Region:       NormalizeRegion(e.Region),
			Name:         e.Name,
			SecretString: e.SecretString,
			CreatedDate:  created,
			VersionID:    vid,
		})
	}
	return nil
}

// DefaultVersionID returns a deterministic mock version id for a secret name.
func DefaultVersionID(name string) string {
	h := sha256.Sum256([]byte(name + "-version"))
	return strings.ToUpper(hex.EncodeToString(h[:16]))
}

func isHex(s string) bool {
	_, err := hex.DecodeString(s)
	return err == nil && len(s)%2 == 0
}

// ParseNameFromSecretID normalizes SecretId from CLI/SDK (name, ARN, or name-suffix).
func ParseNameFromSecretID(secretID string) string {
	if strings.HasPrefix(secretID, "arn:") {
		idx := strings.Index(secretID, ":secret:")
		if idx >= 0 {
			secretID = secretID[idx+len(":secret:"):]
		}
	}
	if i := strings.LastIndex(secretID, "-"); i > 0 {
		tail := secretID[i+1:]
		if len(tail) == 8 && isHex(tail) {
			return secretID[:i]
		}
	}
	return secretID
}

// Lookup returns the secret in DefaultRegion (tests and helpers). Prefer LookupInRegion for the real signing region.
func (s *Store) Lookup(secretID string) *SecretRecord {
	return s.LookupInRegion(secretID, DefaultRegion)
}

// LookupInRegion finds a secret by name/ARN/ARN-style id for the request signing region.
// The secret must have been stored for that same region, or the lookup returns nil.
func (s *Store) LookupInRegion(secretID, requestRegion string) *SecretRecord {
	if s == nil {
		return nil
	}
	name := ParseNameFromSecretID(secretID)
	if name == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.byKey[regionStorageKey(requestRegion, name)]
}

// Count returns the number of secrets in the store.
func (s *Store) Count() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.byKey)
}

func (s *Store) secretNameExistsAnyRegionLocked(name string) bool {
	want := strings.TrimSpace(name)
	if want == "" {
		return false
	}
	for _, rec := range s.byKey {
		if rec != nil && strings.TrimSpace(rec.Name) == want {
			return true
		}
	}
	return false
}

// SeedDefaults adds built-in demo secrets in DefaultRegion for well-known names that have
// no record in the store in any region. (If a name exists only in another region, we do
// not add a second copy in DefaultRegion—that would make wrong-region clients appear to "find" the secret.)
func SeedDefaults(store *Store) {
	if store == nil {
		return
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if !store.secretNameExistsAnyRegionLocked("my-test-secret") {
		store.putLocked(&SecretRecord{
			Region:       DefaultRegion,
			Name:         "my-test-secret",
			SecretString: `{"username":"demo","password":"local-mock"}`,
			CreatedDate:  time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC),
			VersionID:    DefaultVersionID("my-test-secret"),
		})
	}
	if !store.secretNameExistsAnyRegionLocked("other-secret") {
		store.putLocked(&SecretRecord{
			Region:       DefaultRegion,
			Name:         "other-secret",
			SecretString: "plain-string-secret",
			CreatedDate:  time.Now().UTC().Add(-time.Hour),
			VersionID:    DefaultVersionID("other-secret"),
		})
	}
}

// ARNSuffix returns a short deterministic suffix for mock ARNs.
func ARNSuffix(name string) string {
	h := sha256.Sum256([]byte(name))
	return strings.ToLower(hex.EncodeToString(h[:4]))
}

// SynthesizeARN builds a fake ARN for responses.
func SynthesizeARN(region, name string) string {
	return fmt.Sprintf("arn:aws:secretsmanager:%s:000000000000:secret:%s-%s", region, name, ARNSuffix(name))
}

// CreatedDateFloat formats CreatedDate like AWS JSON (Unix seconds as float).
func CreatedDateFloat(t time.Time) float64 {
	return float64(t.Unix()) + float64(t.Nanosecond())/1e9
}
