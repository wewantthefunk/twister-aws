package ec2

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"os"
)

// Service implements the EC2 Query API (form-encoded POST, XML responses).
type Service struct {
	store       *Store
	catalogPath string
	publicHost  string
}

// NewService creates an EC2 handler with on-disk state under dataRoot and AMI catalog at catalogPath (full path).
func NewService(dataRoot, catalogPath, publicHost string) (*Service, error) {
	if err := os.MkdirAll(filepath.Clean(dataRoot), 0o750); err != nil {
		return nil, err
	}
	st, err := NewStore(filepath.Join(filepath.Clean(dataRoot), "state.json"))
	if err != nil {
		return nil, err
	}
	ph := strings.TrimSpace(publicHost)
	if ph == "" {
		ph = "127.0.0.1"
	}
	return &Service{store: st, catalogPath: filepath.Clean(catalogPath), publicHost: ph}, nil
}

func newResourceID(prefix string) string {
	b := make([]byte, 9)
	_, _ = io.ReadFull(rand.Reader, b)
	h := fmt.Sprintf("%x", b)
	if len(h) > 17 {
		h = h[:17]
	}
	return prefix + h
}

func rsaFingerprintMD5(pub *rsa.PublicKey) (string, error) {
	pubDER, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", err
	}
	sum := md5.Sum(pubDER) //nolint:gosec // AWS EC2 legacy fingerprint algorithm
	parts := make([]string, len(sum))
	for i, b := range sum {
		parts[i] = fmt.Sprintf("%02x", b)
	}
	return strings.Join(parts, ":"), nil
}

// Handle serves EC2 after SigV4 verification (credential scope service "ec2").
func (s *Service) Handle(w http.ResponseWriter, r *http.Request, region string, body []byte, rid string) {
	if s == nil || s.store == nil {
		writeAPIError(w, http.StatusInternalServerError, "Unavailable", "ec2 not configured", rid)
		return
	}
	region = strings.TrimSpace(region)
	if region == "" {
		region = "us-east-1"
	}

	ct := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	if !strings.HasPrefix(ct, "application/x-www-form-urlencoded") {
		writeAPIError(w, http.StatusBadRequest, "InvalidParameterValue", "Content-Type must be application/x-www-form-urlencoded", rid)
		return
	}

	q, err := url.ParseQuery(string(body))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "MalformedQueryString", "invalid form body", rid)
		return
	}

	if v := q.Get("Version"); v != "" && v != ec2QueryVersion {
		writeAPIError(w, http.StatusBadRequest, "InvalidParameter", fmt.Sprintf("unsupported Version %q (expected %s)", v, ec2QueryVersion), rid)
		return
	}

	action := strings.TrimSpace(q.Get("Action"))
	if action == "" {
		writeAPIError(w, http.StatusBadRequest, "MissingAction", "Action is required", rid)
		return
	}

	switch action {
	case "CreateKeyPair":
		s.createKeyPair(w, q, region, rid)
	case "CreateVpc":
		s.createVPC(w, q, region, rid)
	case "CreateSubnet":
		s.createSubnet(w, q, region, rid)
	case "DescribeVpcs":
		s.describeVPCs(w, q, region, rid)
	case "DescribeSubnets":
		s.describeSubnets(w, q, region, rid)
	case "CreateSecurityGroup":
		s.createSecurityGroup(w, q, region, rid)
	case "AuthorizeSecurityGroupIngress":
		s.authorizeSecurityGroupIngress(w, q, region, rid)
	case "DescribeSecurityGroups":
		s.describeSecurityGroups(w, q, region, rid)
	case "RunInstances":
		s.runInstances(w, r, q, region, rid)
	case "DescribeInstances":
		s.describeInstances(w, q, region, rid)
	case "TerminateInstances":
		s.terminateInstances(w, r, q, region, rid)
	case "StopInstances":
		s.stopInstances(w, r, q, region, rid)
	case "StartInstances":
		s.startInstances(w, r, q, region, rid)
	default:
		writeAPIError(w, http.StatusBadRequest, "InvalidAction", "unsupported Action "+action, rid)
	}
}

type createKeyPairResponse struct {
	XMLName        xml.Name `xml:"CreateKeyPairResponse"`
	Xmlns          string   `xml:"xmlns,attr"`
	RequestId      string   `xml:"requestId"`
	KeyName        string   `xml:"keyName"`
	KeyFingerprint string   `xml:"keyFingerprint"`
	KeyMaterial    string   `xml:"keyMaterial"`
	KeyPairId      string   `xml:"keyPairId"`
}

// ReloadState reloads state.json from disk (for example after /refresh).
func (s *Service) ReloadState() error {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.Reload()
}
