package file

import (
	"context"
	"crypto/sha256"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fullstacks-gmbh/airgapper/internal/config"
	"github.com/fullstacks-gmbh/airgapper/internal/credentials"
	"github.com/fullstacks-gmbh/airgapper/internal/domain"
	"github.com/fullstacks-gmbh/airgapper/internal/sync"
	"github.com/fullstacks-gmbh/airgapper/internal/transport/retry"
)

func TestFileSync(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		exists, force, dry, badHash bool
		headStatus, putStatus       int
		synced, skipped, failed     int
		gets, puts                  int
	}{
		{name: "copy", synced: 1, gets: 1, puts: 1},
		{name: "skip", exists: true, skipped: 1},
		{name: "force", exists: true, force: true, synced: 1, gets: 1, puts: 1},
		{name: "dry missing", dry: true, skipped: 1},
		{name: "dry force", exists: true, force: true, dry: true, skipped: 1},
		{name: "checksum mismatch", badHash: true, failed: 1, gets: 1},
		{name: "unauthorized", headStatus: 401, failed: 1},
		{name: "server error", headStatus: 500, failed: 1},
		{name: "upload error", putStatus: 403, failed: 1, gets: 1, puts: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TMPDIR", t.TempDir())
			const content = "binary\x00archive"
			digest := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
			gets, puts := 0, 0
			src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gets++
				user, pass, _ := r.BasicAuth()
				if user != "source" || pass != "src-token" {
					t.Error("missing source auth")
				}
				_, _ = io.WriteString(w, content)
			}))
			defer src.Close()
			dst := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				user, pass, _ := r.BasicAuth()
				if user != "target" || pass != "dst-token" {
					t.Error("missing target auth")
				}
				switch r.Method {
				case http.MethodHead:
					if tc.headStatus != 0 {
						w.WriteHeader(tc.headStatus)
					} else if !tc.exists {
						w.WriteHeader(404)
					}
				case http.MethodPut:
					puts++
					data, err := io.ReadAll(r.Body)
					if err != nil {
						t.Error(err)
					}
					if string(data) != content || r.ContentLength != int64(len(content)) || r.Header.Get("X-Checksum-Sha256") != digest {
						t.Error("incorrect upload content/length/digest")
					}
					if tc.putStatus != 0 {
						w.WriteHeader(tc.putStatus)
					} else {
						w.Header().Set("X-Checksum-Sha256", digest)
						w.WriteHeader(201)
					}
				default:
					t.Errorf("unexpected method %s", r.Method)
				}
			}))
			defer dst.Close()
			rc := config.ResourceConfig{Type: "file", Source: src.URL + "/archive.tar.gz", Destination: dst.URL + "/repo/v1/archive.tar.gz", SHA256: digest, SourceCredentialsRef: "upstream", TargetCredentialsRef: "artifactory"}
			if tc.force {
				rc.PushMode = "force"
			}
			if tc.badHash {
				rc.SHA256 = strings.Repeat("0", 64)
			}
			if err := config.Validate(&config.Config{Resources: []config.ResourceConfig{rc}}); err != nil {
				t.Fatal(err)
			}
			store := credentials.NewFileStore([]domain.Credential{
				{Name: "upstream", Type: domain.CredentialTypeFile, Username: "source", Password: "src-token"},
				{Name: "artifactory", Type: domain.CredentialTypeFile, Username: "target", Password: "dst-token"},
			})
			tr := New(nil)
			engine := sync.NewEngine([]domain.Transporter{tr}, nil, slog.Default())
			results, err := engine.Run(context.Background(), []domain.Resource{rc.ToResource()}, domain.SyncOptions{Credentials: store, DryRun: tc.dry})
			if err != nil {
				t.Fatal(err)
			}
			result := results[0]
			if len(result.Synced) != tc.synced || len(result.Skipped) != tc.skipped || len(result.Failed) != tc.failed {
				t.Fatalf("unexpected result: %+v", result)
			}
			if gets != tc.gets || puts != tc.puts {
				t.Fatalf("GET=%d PUT=%d", gets, puts)
			}
			entries, err := os.ReadDir(os.Getenv("TMPDIR"))
			if err != nil || len(entries) != 0 {
				t.Fatalf("spool not cleaned: %v %v", entries, err)
			}
		})
	}
}

func TestRetryRewindsFiles(t *testing.T) {
	gets, puts := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			w.WriteHeader(404)
		case http.MethodGet:
			gets++
			if gets == 1 {
				w.WriteHeader(503)
				return
			}
			_, _ = io.WriteString(w, "payload")
		case http.MethodPut:
			puts++
			body, err := io.ReadAll(r.Body)
			if err != nil || string(body) != "payload" {
				t.Error("retry did not rewind body")
			}
			if puts == 1 {
				w.WriteHeader(503)
				return
			}
			w.WriteHeader(201)
		}
	}))
	defer server.Close()
	resource := (&config.ResourceConfig{Type: "file", Source: server.URL + "/src", Destination: server.URL + "/dst"}).ToResource()
	result, err := New(nil).WithRetry(retry.Policy{Attempts: 2}).Sync(context.Background(), resource, domain.SyncOptions{})
	if err != nil || len(result.Synced) != 1 || gets != 2 || puts != 2 {
		t.Fatalf("retry failed: %+v %v %d %d", result, err, gets, puts)
	}
}

func TestRedirectCredentialIsolation(t *testing.T) {
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("credentials leaked to other origin")
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer sink.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, sink.URL+"/signed?secret=hidden", 302)
	}))
	defer origin.Close()
	client, transport, err := clientFor(domain.Endpoint{})
	if err != nil {
		t.Fatal(err)
	}
	defer transport.CloseIdleConnections()
	resp, err := request(context.Background(), client, "GET", origin.URL, nil, 0, &domain.Credential{Username: "u", Password: "secret"}, "")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	resp, err = request(context.Background(), client, "PUT", origin.URL, strings.NewReader("x"), 1, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 302 {
		t.Fatal("upload redirect followed")
	}
}

func TestTLSAndCancellation(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer server.Close()
	tr := New(nil)
	endpoint := domain.Endpoint{Repository: server.URL + "/file"}
	if _, err := tr.Exists(context.Background(), endpoint, "", nil); err == nil {
		t.Fatal("untrusted certificate accepted")
	}
	dir := t.TempDir()
	cert := server.Certificate()
	if err := os.WriteFile(filepath.Join(dir, "ca.crt"), []byte(string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}))), 0600); err != nil {
		t.Fatal(err)
	}
	endpoint.CACertPath = dir
	if ok, err := tr.Exists(context.Background(), endpoint, "", nil); err != nil || !ok {
		t.Fatalf("CA trust failed: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tr.Exists(ctx, endpoint, "", nil); err == nil {
		t.Fatal("cancellation ignored")
	}
}

func TestDownloadCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			w.WriteHeader(404)
			return
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	resource := (&config.ResourceConfig{Type: "file", Source: server.URL + "/src", Destination: server.URL + "/dst"}).ToResource()
	result, err := New(nil).Sync(ctx, resource, domain.SyncOptions{})
	if err != nil || len(result.Failed) != 1 {
		t.Fatalf("expected cancelled download: %+v %v", result, err)
	}
}

func TestPreservesEncodedAndEmptyContent(t *testing.T) {
	for _, content := range []string{"", "\x1f\x8braw gzip bytes"} {
		t.Run(fmt.Sprintf("size-%d", len(content)), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case "HEAD":
					w.WriteHeader(404)
				case "GET":
					w.Header().Set("Content-Encoding", "gzip")
					_, _ = io.WriteString(w, content)
				case "PUT":
					data, err := io.ReadAll(r.Body)
					if err != nil || string(data) != content || r.ContentLength != int64(len(content)) {
						t.Error("artifact bytes or content length changed")
					}
					w.WriteHeader(201)
				}
			}))
			defer server.Close()
			resource := (&config.ResourceConfig{Type: "file", Source: server.URL + "/src", Destination: server.URL + "/dst"}).ToResource()
			result, err := New(nil).Sync(context.Background(), resource, domain.SyncOptions{})
			if err != nil || len(result.Synced) != 1 {
				t.Fatalf("copy failed: %+v %v", result, err)
			}
		})
	}
}
