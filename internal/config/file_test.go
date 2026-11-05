package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fullstacks-gmbh/airgapper/internal/domain"
)

func TestFileConfigValidation(t *testing.T) {
	for _, kind := range []string{"file", "generic"} {
		rc := ResourceConfig{Type: kind, Source: "https://github.com/rancher/rke2/releases/download/v1%2Br1/archive.tar.gz", Destination: "https://artifactory.example/artifactory/generic/rke2/v1+r1/archive.tar.gz", SHA256: strings.Repeat("a", 64)}
		cfg := &Config{Resources: []ResourceConfig{rc}}
		if err := Validate(cfg); err != nil {
			t.Fatal(err)
		}
		res := rc.ToResource()
		if res.Type != domain.ResourceTypeFile || res.Source.Repository != rc.Source || res.Destination.Repository != rc.Destination || len(res.Versions) != 1 || res.SHA256 != rc.SHA256 {
			t.Fatalf("conversion failed: %+v", res)
		}
		for _, mutate := range []func(*ResourceConfig){
			func(r *ResourceConfig) { r.Source = "" },
			func(r *ResourceConfig) { r.Destination = "ftp://host/file" },
			func(r *ResourceConfig) { r.Source = "https://user:secret@host/file" },
			func(r *ResourceConfig) { r.Source = "https://host/file?token=secret" },
			func(r *ResourceConfig) { r.Destination = "https://host/repository/" },
			func(r *ResourceConfig) { r.SHA256 = "bad" },
			func(r *ResourceConfig) { r.Versions = []string{".*"} },
			func(r *ResourceConfig) { r.ScannerRef = "scanner" },
			func(r *ResourceConfig) { r.PushMode = "typo" },
		} {
			bad := rc
			mutate(&bad)
			if err := Validate(&Config{Resources: []ResourceConfig{bad}}); err == nil {
				t.Fatalf("accepted invalid config: %+v", bad)
			}
		}
	}
}

func TestFileCredentialsLoadingAndCheck(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.creds.airgapper.yaml")
	if err := os.WriteFile(path, []byte("file:\n  - name: artifactory\n    username: svc-airgapper\n    password: dummy\n"), 0600); err != nil {
		t.Fatal(err)
	}
	creds, err := LoadCredentials(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(creds) != 1 || creds[0].Type != domain.CredentialTypeFile {
		t.Fatalf("incorrect credentials: %+v", creds)
	}
	cfg := &Config{Resources: []ResourceConfig{{Type: "file", Source: "https://host/file", Destination: "https://host/repo/file", TargetCredentialsRef: "artifactory"}}}
	if err := Check(cfg, creds, true); err != nil {
		t.Fatal(err)
	}
	if err := Check(cfg, nil, true); err == nil {
		t.Fatal("missing file credentials accepted")
	}
}
