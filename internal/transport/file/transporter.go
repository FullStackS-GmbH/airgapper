// Package file copies arbitrary HTTP files to repositories supporting HTTP PUT.
package file

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/fullstacks-gmbh/airgapper/internal/domain"
	airtransport "github.com/fullstacks-gmbh/airgapper/internal/transport"
	"github.com/fullstacks-gmbh/airgapper/internal/transport/registry"
	"github.com/fullstacks-gmbh/airgapper/internal/transport/retry"
)

// Transporter synchronizes exact file URLs, spooling content to disk to bound memory.
type Transporter struct {
	logger *slog.Logger
	retry  retry.Policy
}

var _ domain.Transporter = (*Transporter)(nil)

// New creates a file transporter.
func New(logger *slog.Logger) *Transporter {
	if logger == nil {
		logger = slog.Default()
	}
	return &Transporter{logger: logger}
}

// WithRetry sets the retry policy before concurrent use.
func (t *Transporter) WithRetry(policy retry.Policy) *Transporter { t.retry = policy; return t }

// Type returns the canonical file resource type.
func (t *Transporter) Type() domain.ResourceType { return domain.ResourceTypeFile }

// ListVersions is unsupported: files use exact URLs, without discovery or templates.
func (t *Transporter) ListVersions(context.Context, domain.Endpoint, *domain.Credential) ([]string, error) {
	return nil, fmt.Errorf("file URLs do not support version discovery: %w", domain.ErrUnsupportedTransport)
}

type statusError int

func (s statusError) Error() string   { return fmt.Sprintf("HTTP status %d", s) }
func (s statusError) HTTPStatus() int { return int(s) }

func clientFor(endpoint domain.Endpoint) (*http.Client, *http.Transport, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Preserve artifact bytes even when the source labels them Content-Encoding: gzip.
	transport.DisableCompression = true
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: endpoint.Insecure} // Explicit endpoint opt-in.
	if endpoint.CACertPath != "" {
		pool, err := x509.SystemCertPool()
		if err != nil {
			return nil, nil, fmt.Errorf("system CA pool: %w", err)
		}
		pem, err := os.ReadFile(filepath.Join(endpoint.CACertPath, "ca.crt"))
		if err != nil {
			return nil, nil, fmt.Errorf("read endpoint CA: %w", err)
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, nil, errors.New("endpoint CA contains no certificates")
		}
		transport.TLSClientConfig.RootCAs = pool
	}
	client := &http.Client{Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		first := via[0]
		if first.Method != http.MethodGet {
			return http.ErrUseLastResponse
		}
		if via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return errors.New("refusing HTTPS downgrade")
		}
		// Go forwards Authorization to subdomains by default; file credentials must
		// stay on the exact original origin, including its port.
		if req.URL.Scheme != first.URL.Scheme || !strings.EqualFold(req.URL.Host, first.URL.Host) {
			req.Header.Del("Authorization")
		}
		return nil
	}}
	return client, transport, nil
}

func request(ctx context.Context, client *http.Client, method, rawURL string, body io.Reader, size int64, cred *domain.Credential, digest string) (*http.Response, error) {
	if method == http.MethodPut && size == 0 {
		body = http.NoBody
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, errors.New("invalid file request URL")
	}
	if cred != nil {
		req.SetBasicAuth(cred.Username, cred.Password)
	}
	if method == http.MethodPut {
		req.ContentLength = size
		req.Header.Set("Content-Type", "application/octet-stream")
		req.Header.Set("X-Checksum-Sha256", digest)
	}
	resp, err := client.Do(req)
	if err != nil {
		// A redirected download URL can contain signed query parameters. Keep URLs
		// and response bodies out of errors, including retry logs.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			return nil, fmt.Errorf("file HTTP request: %w", urlErr.Err)
		}
		return nil, errors.New("file HTTP request failed")
	}
	return resp, nil
}

// Exists checks a destination with HEAD. Only 404 means missing; auth and server
// errors fail the operation instead of being mistaken for absent files.
func (t *Transporter) Exists(ctx context.Context, endpoint domain.Endpoint, _ string, cred *domain.Credential) (bool, error) {
	client, transport, err := clientFor(endpoint)
	if err != nil {
		return false, err
	}
	defer transport.CloseIdleConnections()
	var exists bool
	err = t.retry.Do(ctx, t.logger, "check file", func() error {
		resp, err := request(ctx, client, http.MethodHead, endpoint.Repository, nil, 0, cred, "")
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusNotFound:
			exists = false
			return nil
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			exists = true
			return nil
		default:
			return statusError(resp.StatusCode)
		}
	})
	return exists, err
}

// Sync downloads a file, optionally verifies SHA-256, then uploads with PUT.
// Dry runs never download or upload. Existing files follow skip/force semantics.
func (t *Transporter) Sync(ctx context.Context, resource domain.Resource, opts domain.SyncOptions) (*domain.SyncResult, error) {
	result := airtransport.SyncVersions(resource.Versions, func(version string) (domain.VersionResult, []domain.OperationRecord) {
		op := func(kind domain.OperationType, msg string) domain.OperationRecord {
			return domain.OperationRecord{ResourceType: domain.ResourceTypeFile, Operation: kind, Source: resource.Source.String(), Destination: resource.Destination.String(), Version: version, Message: msg}
		}
		failed := func(err error) (domain.VersionResult, []domain.OperationRecord) {
			return domain.VersionResult{Version: version, Status: domain.SyncStatusFailed, Error: err, Message: err.Error()}, []domain.OperationRecord{op(domain.OpFail, err.Error())}
		}
		resolve := func(ref string, endpoint domain.Endpoint) (*domain.Credential, error) {
			u, err := url.Parse(endpoint.Repository)
			if err != nil {
				return nil, errors.New("invalid file endpoint")
			}
			return registry.ResolveCredentials(ref, u.Host, domain.CredentialTypeFile, opts.Credentials)
		}
		dstCred, err := resolve(resource.TargetCredentialsRef, resource.Destination)
		if err != nil {
			return failed(err)
		}
		exists, err := t.Exists(ctx, resource.Destination, version, dstCred)
		if err != nil {
			return failed(fmt.Errorf("check destination: %w", err))
		}
		if opts.DryRun {
			return airtransport.DryRunResult(resource.PushMode, version, exists, t.logger, op)
		}
		if exists && resource.PushMode != domain.PushModeForce {
			return domain.VersionResult{Version: version, Status: domain.SyncStatusSkipped, Message: "already exists"}, []domain.OperationRecord{op(domain.OpSkip, "already exists")}
		}
		srcCred, err := resolve(resource.SourceCredentialsRef, resource.Source)
		if err != nil {
			return failed(err)
		}
		if err := t.copy(ctx, resource, srcCred, dstCred); err != nil {
			return failed(err)
		}
		kind := domain.OpPush
		if exists {
			kind = domain.OpOverwrite
		}
		return domain.VersionResult{Version: version, Status: domain.SyncStatusSynced, Message: "uploaded"}, []domain.OperationRecord{op(domain.OpPull, "downloaded and verified"), op(kind, "uploaded")}
	})
	result.Resource = resource
	return result, nil
}

func (t *Transporter) copy(ctx context.Context, resource domain.Resource, srcCred, dstCred *domain.Credential) error {
	src, srcTransport, err := clientFor(resource.Source)
	if err != nil {
		return err
	}
	defer srcTransport.CloseIdleConnections()
	dst, dstTransport, err := clientFor(resource.Destination)
	if err != nil {
		return err
	}
	defer dstTransport.CloseIdleConnections()
	spool, err := os.CreateTemp("", "airgapper-file-*")
	if err != nil {
		return fmt.Errorf("create file spool: %w", err)
	}
	defer os.Remove(spool.Name())
	defer spool.Close()
	var digest string
	var size int64
	err = t.retry.Do(ctx, t.logger, "download file", func() error {
		if err := spool.Truncate(0); err != nil {
			return err
		}
		if _, err := spool.Seek(0, io.SeekStart); err != nil {
			return err
		}
		resp, err := request(ctx, src, http.MethodGet, resource.Source.Repository, nil, 0, srcCred, "")
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return statusError(resp.StatusCode)
		}
		hash := sha256.New()
		size, err = io.Copy(io.MultiWriter(spool, hash), resp.Body)
		if err != nil {
			return err
		}
		digest = fmt.Sprintf("%x", hash.Sum(nil))
		if resource.SHA256 != "" && !strings.EqualFold(digest, resource.SHA256) {
			return errors.New("file SHA-256 mismatch; refusing upload")
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("download file: %w", err)
	}
	err = t.retry.Do(ctx, t.logger, "upload file", func() error {
		if _, err := spool.Seek(0, io.SeekStart); err != nil {
			return err
		}
		// The wrapper prevents net/http from closing the shared spool after a try.
		resp, err := request(ctx, dst, http.MethodPut, resource.Destination.Repository, struct{ io.Reader }{spool}, size, dstCred, digest)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return statusError(resp.StatusCode)
		}
		if reported := resp.Header.Get("X-Checksum-Sha256"); reported != "" && !strings.EqualFold(reported, digest) {
			return errors.New("destination SHA-256 mismatch")
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("upload file: %w", err)
	}
	return nil
}
