package credentials

import (
	"encoding/csv"
	"os"
	"strings"
)

// LoadCSV loads access_key_id → secret_access_key pairs from a CSV file.
// Expected columns: access_key_id, secret_access_key (header row optional).
// Later rows with the same access key overwrite earlier ones.
func LoadCSV(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}

	out := make(map[string]string)
	start := 0
	if len(records) > 0 && len(records[0]) >= 2 {
		h := strings.ToLower(strings.TrimSpace(records[0][0]))
		if h == "access_key_id" || h == "access key id" {
			start = 1
		}
	}

	for _, row := range records[start:] {
		if len(row) < 2 {
			continue
		}
		ak := strings.TrimSpace(row[0])
		sk := strings.TrimSpace(row[1])
		if ak == "" || sk == "" {
			continue
		}
		out[ak] = sk
	}
	return out, nil
}
