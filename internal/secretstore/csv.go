package secretstore

import (
	"encoding/csv"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

// LoadSecretsCSV loads secret records from a CSV file into the store.
// Header row is optional. Recognized columns (case-insensitive):
//   - name (required)
//   - secretString or secret_string (required)
//   - createdDate or created_date (optional; RFC3339 or Unix seconds)
//   - versionId or version_id (optional; empty → DefaultVersionID(name))
//   - region (optional; empty → DefaultRegion) — must match the client signing region for GetSecretValue
//
// Unheaded rows: 2, 3, or 4 columns behave as before (no region, DefaultRegion);
// 5+ columns: name, secretString, createdDate, versionId, region.
// Later (region, name) pairs: later row wins. Empty lines and rows with a blank name are skipped.
func LoadSecretsCSV(path string, store *Store) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	records, err := r.ReadAll()
	if err != nil {
		return err
	}
	if len(records) == 0 {
		return nil
	}

	header := mapColumnIndex(records[0])
	start := 0
	if len(header) > 0 {
		start = 1
	} else {
		header = defaultSecretColumnHeader()
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	for _, row := range records[start:] {
		name := getCol(row, header, "name")
		if strings.TrimSpace(name) == "" {
			continue
		}
		ss := getCol(row, header, "secretstring")
		if ss == "" {
			ss = getCol(row, header, "secret_string")
		}
		if strings.TrimSpace(ss) == "" {
			continue
		}

		created := time.Now().UTC()
		rawDate := getCol(row, header, "createddate")
		if rawDate == "" {
			rawDate = getCol(row, header, "created_date")
		}
		if strings.TrimSpace(rawDate) != "" {
			if t, err := time.Parse(time.RFC3339, strings.TrimSpace(rawDate)); err == nil {
				created = t.UTC()
			} else if u, err := strconv.ParseFloat(strings.TrimSpace(rawDate), 64); err == nil {
				sec, frac := math.Modf(u)
				created = time.Unix(int64(sec), int64(frac*1e9)).UTC()
			}
		}

		vid := getCol(row, header, "versionid")
		if vid == "" {
			vid = getCol(row, header, "version_id")
		}
		if strings.TrimSpace(vid) == "" {
			vid = DefaultVersionID(name)
		}

		region := strings.TrimSpace(getCol(row, header, "region"))

		store.putLocked(&SecretRecord{
			Region:       NormalizeRegion(region),
			Name:         strings.TrimSpace(name),
			SecretString: ss,
			CreatedDate:  created,
			VersionID:    strings.TrimSpace(vid),
		})
	}
	return nil
}

func defaultSecretColumnHeader() map[string]int {
	// no header: name, secretString [, createdDate [, versionId [, region]]]
	return map[string]int{
		"name":         0,
		"secretstring": 1,
		"createddate":  2,
		"versionid":    3,
		"region":       4,
	}
}

// mapColumnIndex returns lowercase column name → index, or nil if the row does not look like a header.
func mapColumnIndex(row []string) map[string]int {
	if len(row) < 2 {
		return nil
	}
	m := make(map[string]int)
	for i, c := range row {
		k := strings.ToLower(strings.TrimSpace(c))
		if k == "" {
			continue
		}
		m[k] = i
	}
	if _, n := m["name"]; !n {
		return nil
	}
	hasSecret := false
	for _, key := range []string{"secretstring", "secret_string"} {
		if _, ok := m[key]; ok {
			hasSecret = true
			break
		}
	}
	if !hasSecret {
		return nil
	}
	return m
}

func getCol(row []string, header map[string]int, key string) string {
	i, ok := header[strings.ToLower(key)]
	if !ok || i >= len(row) {
		return ""
	}
	return row[i]
}
