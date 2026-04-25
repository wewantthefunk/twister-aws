package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/christian/twister/internal/awsserver"
	"github.com/christian/twister/internal/config"
	"github.com/christian/twister/internal/credentials"
	"github.com/christian/twister/internal/iam"
	"github.com/christian/twister/internal/paramstore"
	"github.com/christian/twister/internal/s3buckets"
	"github.com/christian/twister/internal/secretsmanager"
	"github.com/christian/twister/internal/secretstore"
	"github.com/christian/twister/internal/ssm"
)

// getenvFirst returns the first non-empty environment variable among keys, or fallback.
func getenvFirst(keys []string, fallback string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return fallback
}

func main() {
	cfgPath := getenvFirst([]string{"TWISTER_SERVER_CONFIG", "SECRETS_LOCAL_SERVER_CONFIG"}, "server.json")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("load server config %s: %v", cfgPath, err)
	}

	dataPath := getenvFirst([]string{"TWISTER_DATA_PATH", "SECRETS_LOCAL_DATA_PATH"}, cfg.DataPath)
	if s := strings.TrimSpace(dataPath); s != "" {
		if err := os.MkdirAll(filepath.Clean(s), 0o750); err != nil {
			log.Fatalf("dataPath %q: %v", s, err)
		}
	}

	s3Root := getenvFirst([]string{"TWISTER_S3_DATA_PATH"}, config.ResolveWithDataPath(dataPath, cfg.S3DataPath))
	if err := os.MkdirAll(filepath.Clean(s3Root), 0o750); err != nil {
		log.Fatalf("s3 data root %q: %v", s3Root, err)
	}
	s3mgr := s3buckets.NewManager(s3Root)

	data := secretstore.NewStore()

	secretsCSVPath := getenvFirst([]string{"TWISTER_SECRETS_CSV", "SECRETS_LOCAL_SECRETS_CSV"}, config.ResolveWithDataPath(dataPath, cfg.SecretsCSV))
	if err := secretstore.LoadSecretsCSV(secretsCSVPath, data); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Fatalf("load secrets CSV %s: %v", secretsCSVPath, err)
	}

	secretsPath := config.ResolveWithDataPath(dataPath, cfg.SecretsFile)
	if err := secretstore.LoadSecretsJSON(secretsPath, data); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Fatalf("load %s: %v", secretsPath, err)
	}
	secretstore.SeedDefaults(data)

	pstore := paramstore.NewStore()
	parametersCSVPath := getenvFirst([]string{"TWISTER_PARAMETERS_CSV"}, config.ResolveWithDataPath(dataPath, cfg.ParametersCSV))
	if err := paramstore.LoadParametersCSV(parametersCSVPath, pstore); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Fatalf("load parameters CSV %s: %v", parametersCSVPath, err)
	}
	parametersPath := getenvFirst([]string{"TWISTER_PARAMETERS_JSON"}, config.ResolveWithDataPath(dataPath, cfg.ParametersFile))
	if err := paramstore.LoadParametersJSON(parametersPath, pstore); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Fatalf("load %s: %v", parametersPath, err)
	}

	credPath := getenvFirst([]string{"TWISTER_CREDENTIALS_CSV", "SECRETS_LOCAL_CREDENTIALS_CSV"}, config.ResolveWithDataPath(dataPath, cfg.CredentialsFile))
	provider, err := credentials.FromFile(credPath)
	if err != nil {
		log.Fatalf("load credentials CSV %s: %v", credPath, err)
	}
	if provider.IsEmpty() {
		log.Printf("no credentials in %s — run: aws iam create-access-key --endpoint-url http://<host>/ --region <region>  (adds first key to the allowlist and this file)", credPath)
	}

	pid := os.Getpid()
	pidLogPath := getenvFirst([]string{"TWISTER_PID_FILE", "SECRETS_LOCAL_PID_FILE"}, config.ResolveWithDataPath(dataPath, cfg.PIDFile))
	if err := os.WriteFile(pidLogPath, []byte(fmt.Sprintf("%d\n", pid)), 0o644); err != nil {
		log.Fatalf("write pid log file %s: %v", pidLogPath, err)
	}
	fmt.Fprintf(os.Stdout, "Twister pid %d (also written to %s)\n", pid, pidLogPath)

	router, err := awsserver.NewRouter(provider, iam.New(provider), secretsmanager.New(data, secretsCSVPath), ssm.New(pstore, parametersCSVPath))
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Twister listening on %s (dataPath=%q, s3Root=%q, %d secrets, %d parameters, %d access keys; secrets CSV %s, parameters CSV %s, creds %s)", cfg.ListenAddress, dataPath, s3Root, data.Count(), pstore.Count(), provider.AccessKeyCount(), secretsCSVPath, parametersCSVPath, credPath)

	ref := &awsserver.Refresher{
		Provider:           provider,
		Store:              data,
		SecretsCSVPath:     secretsCSVPath,
		SecretsJSONPath:    secretsPath,
		ParamStore:         pstore,
		ParametersCSVPath:  parametersCSVPath,
		ParametersJSONPath: parametersPath,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", awsserver.Health)
	mux.HandleFunc("/refresh", ref.Refresh)
	mux.Handle("/", &awsserver.PrimaryHandler{Provider: provider, S3: s3mgr, API: router})

	srv := &http.Server{
		Addr:              cfg.ListenAddress,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
