package paramstore

import (
	"encoding/csv"
	"os"
	"strconv"
	"strings"
	"time"
)

// LoadParametersCSV loads parameters from a CSV file. Header is optional; columns
// (case-insensitive, underscores allowed): name, region, value, type, version, lastModified.
// Without a header: name, value [, region [, type [, version [, lastModified (RFC3339)]]]].
func LoadParametersCSV(path string, store *Store) error {
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
		header = defaultParamColumnHeader()
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	for _, row := range records[start:] {
		name := strings.TrimSpace(getCol(row, header, "name"))
		if name == "" {
			continue
		}
		val := getCol(row, header, "value")
		region := strings.TrimSpace(getCol(row, header, "region"))
		rtype := strings.TrimSpace(getCol(row, header, "type"))
		if rtype == "" {
			rtype = "String"
		}
		ver := 1
		if v := getCol(row, header, "version"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				ver = n
			}
		}
		mod := time.Now().UTC()
		if raw := getCol(row, header, "lastModified"); raw != "" {
			if t, err := time.Parse(time.RFC3339, raw); err == nil {
				mod = t.UTC()
			} else if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
				mod = t.UTC()
			}
		}
		store.putLocked(&ParameterRecord{
			Region:       NormalizeRegion(region),
			Name:         name,
			Type:         rtype,
			Value:        val,
			Version:      ver,
			LastModified: mod,
		})
	}
	return nil
}

func defaultParamColumnHeader() map[string]int {
	return map[string]int{
		"name":         0,
		"value":        1,
		"region":       2,
		"type":         3,
		"version":      4,
		"lastmodified": 5,
	}
}

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
	if _, v := m["value"]; !v {
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
