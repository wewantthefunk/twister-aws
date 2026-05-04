package lambda

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// FunctionConfig is the persisted function definition.
type FunctionConfig struct {
	FunctionName  string `json:"FunctionName"`
	Region        string `json:"Region,omitempty"`
	ImageURI      string `json:"ImageUri"`
	Handler       string `json:"Handler,omitempty"`
	Timeout       int    `json:"Timeout,omitempty"`
	MemorySize    int    `json:"MemorySize,omitempty"`
	PackageType   string `json:"PackageType,omitempty"`
	Runtime       string `json:"Runtime,omitempty"`
	Role          string `json:"Role,omitempty"`
	Architectures []string `json:"Architectures,omitempty"`
}

var functionNameRe = regexp.MustCompile(`^[a-zA-Z0-9-_]{1,64}$`)

// Registry stores functions as JSON files: Root/functions/{name}.json
type Registry struct {
	Root string
	mu   sync.Mutex
}

// NewRegistry returns a registry rooted at a directory (e.g. data/lambda).
func NewRegistry(root string) *Registry {
	return &Registry{Root: filepath.Clean(root)}
}

func (r *Registry) functionPath(name string) (string, error) {
	if r == nil || r.Root == "" {
		return "", errors.New("lambda: empty registry root")
	}
	if !functionNameRe.MatchString(name) {
		return "", fmt.Errorf("lambda: invalid function name %q", name)
	}
	return filepath.Join(r.Root, "functions", name+".json"), nil
}

// Put saves a function config (create or update).
func (r *Registry) Put(cfg *FunctionConfig) error {
	if cfg == nil || cfg.FunctionName == "" {
		return errors.New("lambda: FunctionName is required")
	}
	if !functionNameRe.MatchString(cfg.FunctionName) {
		return errors.New("lambda: invalid FunctionName")
	}
	if strings.TrimSpace(cfg.ImageURI) == "" {
		return errors.New("lambda: ImageUri is required for v1 (container image)")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30
	}
	if cfg.Timeout > 900 {
		cfg.Timeout = 900
	}
	if cfg.MemorySize <= 0 {
		cfg.MemorySize = 128
	}
	if cfg.MemorySize > 10240 {
		cfg.MemorySize = 10240
	}
	if cfg.PackageType == "" {
		if cfg.ImageURI != "" {
			cfg.PackageType = "Image"
		}
	}
	p, err := r.functionPath(cfg.FunctionName)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Get returns the function config or nil if not found.
func (r *Registry) Get(name string) (*FunctionConfig, error) {
	p, err := r.functionPath(name)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var c FunctionConfig
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// Delete removes a function definition.
func (r *Registry) Delete(name string) error {
	p, err := r.functionPath(name)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// List returns all function names.
func (r *Registry) List() ([]string, error) {
	if r == nil || r.Root == "" {
		return nil, errors.New("lambda: empty registry root")
	}
	dir := filepath.Join(r.Root, "functions")
	r.mu.Lock()
	defer r.mu.Unlock()
	ents, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if !strings.HasSuffix(n, ".json") {
			continue
		}
		n = strings.TrimSuffix(n, ".json")
		if functionNameRe.MatchString(n) {
			names = append(names, n)
		}
	}
	return names, nil
}
