package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Server holds paths and listen settings for the Twister process.
// Empty fields in JSON are filled with defaults after load.
type Server struct {
	// DataPath, if set, is the root directory for all on-disk data files (secrets, parameters, IAM CSV, optional JSON, PID log).
	// File entries below are always resolved as DataPath + basename (e.g. /var/lib/twister/secrets.csv); empty DataPath uses cwd via JoinDot.
	DataPath        string `json:"dataPath"`
	// MapPath is the **host** directory bound to /app in the container when using `make run` (Docker).
	// The Twister binary does not read this field; it is for tooling only. Not combined with dataPath.
	MapPath         string `json:"mapPath"`
	ListenAddress   string `json:"listenAddress"`
	SecretsCSV      string `json:"secretsCSV"`
	SecretsFile     string `json:"secretsFile"`
	ParametersCSV   string `json:"parametersCSV"`
	ParametersFile  string `json:"parametersFile"`
	CredentialsFile string `json:"credentialsFile"`
	PIDFile         string `json:"pidFile"`
	// S3DataPath is the **directory basename** (with dataPath) or relative path where S3 buckets are stored as subfolders.
	S3DataPath string `json:"s3DataPath"`
	// SQSDataPath is the directory (under dataPath) where per-region SQS queue JSON files are stored.
	SQSDataPath string `json:"sqsDataPath"`
}

// Default is used when a config file is missing or a field is omitted/empty.
var Default = Server{
	DataPath:        "",
	MapPath:         "",
	ListenAddress:   ":8080",
	SecretsCSV:      "secrets.csv",
	SecretsFile:     "secrets.json",
	ParametersCSV:   "parameters.csv",
	ParametersFile:  "parameters.json",
	CredentialsFile: "credentials.csv",
	PIDFile:         "twister.log",
	S3DataPath:      "buckets",
	SQSDataPath:     "sqs",
}

// Load reads JSON from path. If the file does not exist, returns Default with a nil error.
// Partial JSON is merged: missing or empty string fields use defaults.
func Load(path string) (Server, error) {
	c := Default
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return Server{}, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return Server{}, err
	}
	mergeServerDefaults(&c)
	return c, nil
}

func mergeServerDefaults(c *Server) {
	if c.ListenAddress == "" {
		c.ListenAddress = Default.ListenAddress
	}
	if c.SecretsCSV == "" {
		c.SecretsCSV = Default.SecretsCSV
	}
	if c.SecretsFile == "" {
		c.SecretsFile = Default.SecretsFile
	}
	if c.ParametersCSV == "" {
		c.ParametersCSV = Default.ParametersCSV
	}
	if c.ParametersFile == "" {
		c.ParametersFile = Default.ParametersFile
	}
	if c.CredentialsFile == "" {
		c.CredentialsFile = Default.CredentialsFile
	}
	if c.PIDFile == "" {
		c.PIDFile = Default.PIDFile
	}
	if c.S3DataPath == "" {
		c.S3DataPath = Default.S3DataPath
	}
	if c.SQSDataPath == "" {
		c.SQSDataPath = Default.SQSDataPath
	}
}

// JoinDot prepends a relative path with the current working-directory dot segment,
// unless path is already absolute.
func JoinDot(p string) string {
	if p == "" {
		return p
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(".", p)
}

// ResolveWithDataPath returns the path for a data file. If dataPath is non-empty (after trim),
// the result is filepath.Join(Clean(dataPath), Base(configuredFile)) so a canonical directory holds
// all such files. If dataPath is empty, configuredFile is resolved with JoinDot (relative to cwd, or as absolute).
func ResolveWithDataPath(dataPath, configuredFile string) string {
	if strings.TrimSpace(dataPath) == "" {
		return JoinDot(configuredFile)
	}
	if configuredFile == "" {
		return filepath.Clean(strings.TrimSpace(dataPath))
	}
	return filepath.Join(filepath.Clean(strings.TrimSpace(dataPath)), filepath.Base(configuredFile))
}
