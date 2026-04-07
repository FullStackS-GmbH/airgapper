package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fullstacks-gmbh/universal-airgapper/internal/domain"
	"gopkg.in/yaml.v3"
)

// configFilePatterns lists the glob patterns used to discover airgapper
// configuration files within a directory. The preferred pattern is
// *.config.airgapper.yaml; legacy patterns are kept for backward compatibility.
var configFilePatterns = []string{
	"*.config.airgapper.yaml",
	"*.config.airgapper.yml",
}

// credentialFilePatterns lists the glob patterns used to discover airgapper
// credential files within a directory.
var credentialFilePatterns = []string{
	"*.creds.airgapper.yaml",
	"*.creds.airgapper.yml",
}

// Load reads airgapper configuration from the given path. If path points to a
// regular file it is loaded directly. If path points to a directory, all files
// matching the recognised airgapper patterns are discovered and merged.
//
// Multiple files are merged by concatenating their Resources and Scanners
// slices. The returned Config is the union of all discovered files.
func Load(path string) (*Config, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("config path %q: %w", path, err)
	}

	var files []string
	if info.IsDir() {
		files, err = discoverFiles(path, configFilePatterns)
		if err != nil {
			return nil, fmt.Errorf("discovering config files in %q: %w", path, err)
		}
		if len(files) == 0 {
			return nil, fmt.Errorf("no airgapper config files found in %q: %w", path, domain.ErrInvalidConfig)
		}
	} else {
		files = []string{path}
	}

	merged := &Config{}
	for _, f := range files {
		cfg, err := loadSingleFile(f)
		if err != nil {
			return nil, fmt.Errorf("loading %q: %w", f, err)
		}
		merged.Scanners = append(merged.Scanners, cfg.Scanners...)
		merged.Resources = append(merged.Resources, cfg.Resources...)
	}

	return merged, nil
}

// discoverFiles returns all files in dir that match any of the given glob
// patterns, deduplicated and in glob expansion order.
func discoverFiles(dir string, patterns []string) ([]string, error) {
	var found []string
	seen := make(map[string]bool)

	for _, pattern := range patterns {
		matches, err := filepath.Glob(filepath.Join(dir, pattern))
		if err != nil {
			return nil, err
		}
		for _, m := range matches {
			if !seen[m] {
				seen[m] = true
				found = append(found, m)
			}
		}
	}

	return found, nil
}

// loadSingleFile reads and unmarshals a single YAML config file.
func loadSingleFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing YAML: %w", err)
	}

	return &cfg, nil
}

// credentialFile mirrors the YAML structure of a credentials file, where
// credentials are grouped by type.
type credentialFile struct {
	Image []credentialEntry `yaml:"image"`
	Helm  []credentialEntry `yaml:"helm"`
	Git   []credentialEntry `yaml:"git"`
}

// credentialEntry is the YAML representation of a single credential.
type credentialEntry struct {
	Name       string `yaml:"name"`
	Username   string `yaml:"username"`
	Password   string `yaml:"password"`
	SSHKeyPath string `yaml:"ssh_key_path"`
}

// LoadCredentials reads credentials from the given path and returns a flat
// slice of domain.Credential values with the appropriate CredentialType set.
//
// If path points to a regular file it is loaded directly. If path points to a
// directory, all files matching the credential file patterns
// (*.creds.airgapper.yaml / *.creds.airgapper.yml) are discovered and merged.
//
// The expected file format groups credentials by type:
//
//	image:
//	  - name: "docker-hub"
//	    username: "user"
//	    password: "pass"
//	helm:
//	  - name: "helm-reg"
//	    username: "user"
//	    password: "pass"
//	git:
//	  - name: "github"
//	    username: "user"
//	    password: "token"
//	    ssh_key_path: "/path/to/key"
func LoadCredentials(path string) ([]domain.Credential, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("credentials path %q: %w", path, err)
	}

	var files []string
	if info.IsDir() {
		files, err = discoverFiles(path, credentialFilePatterns)
		if err != nil {
			return nil, fmt.Errorf("discovering credential files in %q: %w", path, err)
		}
		if len(files) == 0 {
			return nil, fmt.Errorf("no credential files (*.creds.airgapper.yaml) found in %q", path)
		}
	} else {
		files = []string{path}
	}

	var creds []domain.Credential
	for _, f := range files {
		fileCreds, err := loadSingleCredentialFile(f)
		if err != nil {
			return nil, fmt.Errorf("loading credentials from %q: %w", f, err)
		}
		creds = append(creds, fileCreds...)
	}

	return creds, nil
}

// loadSingleCredentialFile reads and parses a single credentials YAML file.
func loadSingleCredentialFile(path string) ([]domain.Credential, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading credentials file %q: %w", path, err)
	}

	var cf credentialFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return nil, fmt.Errorf("parsing credentials YAML %q: %w", path, err)
	}

	var creds []domain.Credential

	for _, e := range cf.Image {
		creds = append(creds, toDomainCredential(e, domain.CredentialTypeImage))
	}
	for _, e := range cf.Helm {
		creds = append(creds, toDomainCredential(e, domain.CredentialTypeHelm))
	}
	for _, e := range cf.Git {
		creds = append(creds, toDomainCredential(e, domain.CredentialTypeGit))
	}

	return creds, nil
}

// toDomainCredential converts a raw credentialEntry into a domain.Credential
// with the given type. Environment variable expansion is intentionally NOT
// performed here — callers that need it should resolve env vars at usage time.
func toDomainCredential(e credentialEntry, ct domain.CredentialType) domain.Credential {
	return domain.Credential{
		Name:       strings.TrimSpace(e.Name),
		Type:       ct,
		Username:   e.Username,
		Password:   e.Password,
		SSHKeyPath: e.SSHKeyPath,
	}
}
