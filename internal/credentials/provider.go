package credentials

import (
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/christian/twister/internal/sigv4"
)

// Provider holds valid access key id → secret access key pairs used to verify SigV4 on every request.
// It is the server-side credential allowlist (not the caller’s environment).
type Provider struct {
	mu             sync.RWMutex
	accessToSecret map[string]string
	csvPath        string
}

// NewProvider builds a provider from a copy of the key map. The map is not retained as the caller’s pointer.
// csvPath, if set, is used by AddAccessKeyAndPersist; otherwise persistence is not available.
func NewProvider(allowlist map[string]string) *Provider {
	return NewProviderWithPath(allowlist, "")
}

// NewProviderWithPath is like NewProvider and records the CSV path for later persistence.
func NewProviderWithPath(allowlist map[string]string, csvPath string) *Provider {
	cp := normalizeAllowlist(allowlist)
	if len(cp) == 0 {
		cp = make(map[string]string)
	}
	return &Provider{accessToSecret: cp, csvPath: csvPath}
}

// FromFile loads a CSV file and returns a Provider (see LoadCSV for format).
// If the file does not exist, the provider is empty and still bound to path so the first
// new key can be persisted. An existing file with only a header and no data rows is treated as empty.
func FromFile(path string) (*Provider, error) {
	_, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewProviderWithPath(nil, path), nil
		}
		return nil, err
	}
	m, err := LoadCSV(path)
	if err != nil {
		return nil, err
	}
	return NewProviderWithPath(m, path), nil
}

// CredentialCSVPath returns the allowlist file path set at load time (empty if none).
func (p *Provider) CredentialCSVPath() string {
	if p == nil {
		return ""
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.csvPath
}

// IsEmpty reports whether there are no allowlisted access keys (bootstrap: first key via CreateAccessKey).
func (p *Provider) IsEmpty() bool {
	if p == nil {
		return true
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.accessToSecret) == 0
}

// AccessKeyCount returns the number of allowlisted access keys.
func (p *Provider) AccessKeyCount() int {
	if p == nil {
		return 0
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.accessToSecret)
}

// Allowlist returns a shallow copy of the internal map for diagnostics only.
func (p *Provider) Allowlist() map[string]string {
	if p == nil {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make(map[string]string, len(p.accessToSecret))
	for k, v := range p.accessToSecret {
		out[k] = v
	}
	return out
}

// VerifyRequest validates AWS SigV4 for the request using the allowlist. It must be called for every protected API call.
// signingService is the service name in the request’s credential scope (e.g. secretsmanager, iam).
func (p *Provider) VerifyRequest(r *http.Request, body []byte, now time.Time) (region, signingService string, err error) {
	if p == nil {
		return "", "", errors.New("nil credentials provider")
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return sigv4.Verify(r, body, p.accessToSecret, now)
}

// AddAccessKeyAndPersist appends a new key pair, updates the in-memory allowlist, and rewrites the credentials CSV
// (see FromFile / NewProviderWithPath for csvPath).
func (p *Provider) AddAccessKeyAndPersist(accessKeyID, secretAccessKey string) error {
	if p == nil {
		return errors.New("nil credentials provider")
	}
	if strings.TrimSpace(accessKeyID) == "" || strings.TrimSpace(secretAccessKey) == "" {
		return errors.New("access key and secret are required")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.csvPath == "" {
		return errors.New("credentials provider has no CSV path")
	}
	p.accessToSecret[accessKeyID] = secretAccessKey
	return writeAllowlistCSV(p.csvPath, p.accessToSecret)
}

// ReloadFromFile reloads the allowlist from the same CSV path used at startup (or FromFile).
// If the file is missing, the in-memory allowlist is cleared. Persists AddAccessKey’s path.
func (p *Provider) ReloadFromFile() error {
	if p == nil {
		return errors.New("nil credentials provider")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.csvPath == "" {
		return errors.New("credentials provider has no CSV path")
	}
	_, err := os.Stat(p.csvPath)
	if err != nil {
		if os.IsNotExist(err) {
			p.accessToSecret = make(map[string]string)
			return nil
		}
		return err
	}
	m, err := LoadCSV(p.csvPath)
	if err != nil {
		return err
	}
	p.accessToSecret = normalizeAllowlist(m)
	return nil
}
