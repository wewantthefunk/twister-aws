package paramstore

import (
	"errors"
	"os"
)

// ReloadFromFiles clears the store and reloads: parameters CSV, then parameters JSON
// (JSON overlays the same (region, name) keys). Missing files are ignored. Empty paths are skipped.
func (s *Store) ReloadFromFiles(parametersCSV, parametersJSON string) error {
	if s == nil {
		return errors.New("nil store")
	}
	s.mu.Lock()
	s.byKey = make(map[string]*ParameterRecord)
	s.mu.Unlock()
	if parametersCSV != "" {
		if err := LoadParametersCSV(parametersCSV, s); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if parametersJSON != "" {
		if err := LoadParametersJSON(parametersJSON, s); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
