package paramstore

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// UpsertPersist updates memory and rewrites the entire parameters CSV atomically.
func (s *Store) UpsertPersist(csvPath string, rec *ParameterRecord) error {
	if s == nil {
		return fmt.Errorf("nil store")
	}
	if rec == nil || strings.TrimSpace(rec.Name) == "" {
		return fmt.Errorf("name is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.putLocked(rec)
	if strings.TrimSpace(csvPath) == "" {
		return fmt.Errorf("parameters csv path is empty")
	}
	return writeParametersCSVUnlocked(csvPath, s)
}

func writeParametersCSVUnlocked(path string, s *Store) error {
	if len(s.byKey) == 0 {
		return nil
	}
	keys := make([]string, 0, len(s.byKey))
	for k := range s.byKey {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "parameters-*.csv.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	w := csv.NewWriter(tmp)
	_ = w.Write([]string{"name", "region", "value", "type", "version", "lastModified"})
	for _, k := range keys {
		r := s.byKey[k]
		if r == nil {
			continue
		}
		row := []string{
			r.Name,
			r.Region,
			r.Value,
			r.Type,
			fmt.Sprintf("%d", r.Version),
			r.LastModified.UTC().Format(time.RFC3339Nano),
		}
		if err := w.Write(row); err != nil {
			_ = tmp.Close()
			return err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
