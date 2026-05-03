package ec2

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

// Store persists EC2 emulation state in a single JSON file (atomic rewrite).
type Store struct {
	path string
	mu   sync.Mutex
	st   diskState
}

type diskState struct {
	KeyPairs       map[string]keyPairRec       `json:"keyPairs"`
	VPCs           map[string]vpcRec           `json:"vpcs"`
	Subnets        map[string]subnetRec        `json:"subnets"`
	SecurityGroups map[string]securityGroupRec `json:"securityGroups"`
	Instances      map[string]instanceRec      `json:"instances"`
}

type keyPairRec struct {
	Region      string `json:"region"`
	Name        string `json:"name"`
	KeyPairID   string `json:"keyPairId"`
	Fingerprint string `json:"fingerprint"`
}

type vpcRec struct {
	ID        string `json:"id"`
	Region    string `json:"region"`
	CidrBlock string `json:"cidrBlock"`
}

type subnetRec struct {
	ID        string `json:"id"`
	Region    string `json:"region"`
	VpcID     string `json:"vpcId"`
	CidrBlock string `json:"cidrBlock"`
}

type ingressRule struct {
	Protocol string `json:"protocol"`
	FromPort int    `json:"fromPort"`
	ToPort   int    `json:"toPort"`
	CidrIPv4 string `json:"cidrIpv4"`
}

type securityGroupRec struct {
	ID          string        `json:"id"`
	Region      string        `json:"region"`
	VpcID       string        `json:"vpcId"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Ingress     []ingressRule `json:"ingress"`
}

type instanceRec struct {
	ID               string            `json:"id"`
	Region           string            `json:"region"`
	AMI              string            `json:"ami"`
	SubnetID         string            `json:"subnetId"`
	SecurityGroupIDs []string          `json:"securityGroupIds"`
	State            string            `json:"state"`
	PrivateIPAddress string            `json:"privateIpAddress"`
	PublicIPAddress  string            `json:"publicIpAddress"`
	DockerImage      string            `json:"dockerImage"`
	DockerCommand    []string          `json:"dockerCommand,omitempty"`
	ContainerName    string            `json:"containerName"`
	User             string            `json:"user"`
	Tags             map[string]string `json:"tags"`
}

func kpKey(region, name string) string {
	return region + "\x00" + name
}

// NewStore opens or creates state at path (directory must exist).
func NewStore(statePath string) (*Store, error) {
	s := &Store{path: filepath.Clean(statePath)}
	if err := s.load(); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		s.st = emptyState()
		if err := s.saveUnlocked(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func emptyState() diskState {
	return diskState{
		KeyPairs:       map[string]keyPairRec{},
		VPCs:           map[string]vpcRec{},
		Subnets:        map[string]subnetRec{},
		SecurityGroups: map[string]securityGroupRec{},
		Instances:      map[string]instanceRec{},
	}
}

func (s *Store) load() error {
	b, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	var st diskState
	if err := json.Unmarshal(b, &st); err != nil {
		return err
	}
	if st.KeyPairs == nil {
		st.KeyPairs = map[string]keyPairRec{}
	}
	if st.VPCs == nil {
		st.VPCs = map[string]vpcRec{}
	}
	if st.Subnets == nil {
		st.Subnets = map[string]subnetRec{}
	}
	if st.SecurityGroups == nil {
		st.SecurityGroups = map[string]securityGroupRec{}
	}
	if st.Instances == nil {
		st.Instances = map[string]instanceRec{}
	}
	s.st = st
	return nil
}

func (s *Store) saveUnlocked() error {
	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, "ec2-state-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(&s.st); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, s.path)
}

// Update locks the store, applies fn to state, and persists. fn must not retain pointers into state.
func (s *Store) Update(fn func(*diskState) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := fn(&s.st); err != nil {
		return err
	}
	return s.saveUnlocked()
}

// Snapshot returns a shallow copy of disk state under lock (maps are copied, values are not deep-cloned).
func (s *Store) Snapshot() diskState {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := diskState{
		KeyPairs:       map[string]keyPairRec{},
		VPCs:           map[string]vpcRec{},
		Subnets:        map[string]subnetRec{},
		SecurityGroups: map[string]securityGroupRec{},
		Instances:      map[string]instanceRec{},
	}
	for k, v := range s.st.KeyPairs {
		out.KeyPairs[k] = v
	}
	for k, v := range s.st.VPCs {
		out.VPCs[k] = v
	}
	for k, v := range s.st.Subnets {
		out.Subnets[k] = v
	}
	for k, v := range s.st.SecurityGroups {
		out.SecurityGroups[k] = v
	}
	for k, v := range s.st.Instances {
		cp := v
		cp.Tags = maps.Clone(v.Tags)
		cp.SecurityGroupIDs = slices.Clone(v.SecurityGroupIDs)
		cp.DockerCommand = slices.Clone(v.DockerCommand)
		out.Instances[k] = cp
	}
	return out
}

// Reload replaces state from disk (used after external edits).
func (s *Store) Reload() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}
