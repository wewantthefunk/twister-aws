package credentials

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// writeAllowlistCSV writes the full allowlist to path (atomic replace) with a header row.
// Keys are written in sorted order.
func writeAllowlistCSV(path string, allowlist map[string]string) error {
	if path == "" {
		return fmt.Errorf("credentials path is empty")
	}
	keys := make([]string, 0, len(allowlist))
	for k := range allowlist {
		if k != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "credentials-*.csv.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	w := csv.NewWriter(tmp)
	_ = w.Write([]string{"access_key_id", "secret_access_key"})
	for _, k := range keys {
		if err := w.Write([]string{k, allowlist[k]}); err != nil {
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

func normalizeAllowlist(m map[string]string) map[string]string {
	if m == nil {
		return make(map[string]string)
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		if strings.TrimSpace(k) == "" {
			continue
		}
		out[k] = v
	}
	return out
}
