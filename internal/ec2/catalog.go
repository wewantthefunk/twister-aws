package ec2

import (
	"encoding/json"
	"fmt"
	"os"
)

// Catalog maps AMI IDs to Docker run metadata.
type Catalog struct {
	AMIs map[string]AMIEntry `json:"amis"`
}

// AMIEntry describes how to start a container for an AMI id.
type AMIEntry struct {
	DockerImage string   `json:"dockerImage"`
	Command     []string `json:"command"`
	DefaultUser string   `json:"defaultUser"`
}

// ReadCatalog loads an AMI catalog from path. Missing file returns empty catalog (no AMIs).
func ReadCatalog(path string) (*Catalog, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Catalog{AMIs: map[string]AMIEntry{}}, nil
		}
		return nil, err
	}
	var raw Catalog
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("ec2 ami catalog: %w", err)
	}
	if raw.AMIs == nil {
		raw.AMIs = map[string]AMIEntry{}
	}
	return &raw, nil
}

func (c *Catalog) resolve(imageID string) (AMIEntry, error) {
	if c == nil || c.AMIs == nil {
		return AMIEntry{}, fmt.Errorf("no catalog")
	}
	ent, ok := c.AMIs[imageID]
	if !ok {
		return AMIEntry{}, fmt.Errorf("unknown image %q", imageID)
	}
	if ent.DockerImage == "" {
		return AMIEntry{}, fmt.Errorf("ami %q has no dockerImage", imageID)
	}
	if ent.DefaultUser == "" {
		ent.DefaultUser = "root"
	}
	if len(ent.Command) == 0 {
		ent.Command = []string{"/bin/sleep", "infinity"}
	}
	return ent, nil
}
