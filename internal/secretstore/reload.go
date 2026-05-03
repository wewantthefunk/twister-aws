package secretstore

import (
	"errors"
	"os"
)

// ReloadFromFiles clears the store and reloads from the same paths used at startup: secrets CSV,
// then secrets JSON (JSON overlays names from CSV), then SeedDefaults — matching main’s order.
// Missing files are ignored (same as startup). Empty paths are skipped.
func (s *Store) ReloadFromFiles(secretsCSV, secretsJSON string) error {
	if s == nil {
		return errors.New("nil store")
	}
	s.mu.Lock()
	s.byKey = make(map[string]*SecretRecord)
	s.mu.Unlock()

	if secretsCSV != "" {
		if err := LoadSecretsCSV(secretsCSV, s); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if secretsJSON != "" {
		if err := LoadSecretsJSON(secretsJSON, s); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	SeedDefaults(s)
	return nil
}
