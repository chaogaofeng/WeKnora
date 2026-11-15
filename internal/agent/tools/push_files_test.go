package tools

import (
	"context"
	"errors"
	"io"
	"mime/multipart"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// stubPushFilesFileService implements only GetFileURL. generateDownloadURL
// never reaches the other methods, so they panic to surface accidental calls.
type stubPushFilesFileService struct {
	url string
	err error
}

var _ interfaces.FileService = (*stubPushFilesFileService)(nil)

func (s *stubPushFilesFileService) CheckConnectivity(context.Context) error { panic("unexpected") }

func (s *stubPushFilesFileService) SaveFile(
	context.Context, *multipart.FileHeader, uint64, string,
) (string, error) {
	panic("unexpected")
}

func (s *stubPushFilesFileService) SaveBytes(
	context.Context, []byte, uint64, string, bool,
) (string, error) {
	panic("unexpected")
}

func (s *stubPushFilesFileService) GetFile(context.Context, string) (io.ReadCloser, error) {
	panic("unexpected")
}

func (s *stubPushFilesFileService) GetFileURL(context.Context, string) (string, error) {
	return s.url, s.err
}

func (s *stubPushFilesFileService) DeleteFile(context.Context, string) error { panic("unexpected") }

func (s *stubPushFilesFileService) CopyFile(
	context.Context, string, uint64, string,
) (string, error) {
	panic("unexpected")
}

// newPushFilesTestTool builds a tool whose storage backend cannot produce a
// public HTTP URL (local storage without an external domain).
func newPushFilesTestTool(t *testing.T, storagePath string) *PushFilesTool {
	t.Helper()
	// A valid signing key so the HMAC step is exercised when a domain is set.
	t.Setenv("SYSTEM_SIGNING_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("SYSTEM_AES_KEY", "")
	return &PushFilesTool{
		fileService: &stubPushFilesFileService{url: storagePath},
		tenantID:    42,
	}
}

// Local storage with neither PUSH_FILES_EXTERNAL_URL nor APP_EXTERNAL_URL set
// must fail loudly: a download link needs an externally reachable domain, there
// is no host-relative fallback.
func TestGenerateDownloadURLRequiresExternalURLWhenUnset(t *testing.T) {
	t.Setenv("PUSH_FILES_EXTERNAL_URL", "")
	t.Setenv("APP_EXTERNAL_URL", "")

	const storagePath = "local://42/k1/manual.docx"
	tool := newPushFilesTestTool(t, storagePath)

	_, err := tool.generateDownloadURL(context.Background(), &types.Knowledge{
		ID: "k1", TenantID: 42, FilePath: storagePath,
	}, 24)
	if err == nil {
		t.Fatal("generateDownloadURL() error = nil, want an error requiring the external URL to be configured")
	}
	if !strings.Contains(err.Error(), "APP_EXTERNAL_URL") {
		t.Errorf("generateDownloadURL() error = %v, want it to mention APP_EXTERNAL_URL", err)
	}
}

func TestGenerateDownloadURLUsesPushFilesExternalURLWhenSet(t *testing.T) {
	t.Setenv("PUSH_FILES_EXTERNAL_URL", "https://dl.example.com")
	t.Setenv("APP_EXTERNAL_URL", "https://app.example.com")

	const storagePath = "local://42/k1/manual.docx"
	tool := newPushFilesTestTool(t, storagePath)

	got, err := tool.generateDownloadURL(context.Background(), &types.Knowledge{
		ID: "k1", TenantID: 42, FilePath: storagePath,
	}, 24)
	if err != nil {
		t.Fatalf("generateDownloadURL() error = %v", err)
	}
	// PUSH_FILES_EXTERNAL_URL must win over APP_EXTERNAL_URL.
	if want := "https://dl.example.com/api/v1/files/presigned?"; !strings.HasPrefix(got, want) {
		t.Fatalf("generateDownloadURL() = %q, want prefix %q", got, want)
	}
}

func TestGenerateDownloadURLFallsBackToAppExternalURL(t *testing.T) {
	t.Setenv("PUSH_FILES_EXTERNAL_URL", "")
	t.Setenv("APP_EXTERNAL_URL", "https://app.example.com")

	const storagePath = "local://42/k1/manual.docx"
	tool := newPushFilesTestTool(t, storagePath)

	got, err := tool.generateDownloadURL(context.Background(), &types.Knowledge{
		ID: "k1", TenantID: 42, FilePath: storagePath,
	}, 24)
	if err != nil {
		t.Fatalf("generateDownloadURL() error = %v", err)
	}
	if want := "https://app.example.com/api/v1/files/presigned?"; !strings.HasPrefix(got, want) {
		t.Fatalf("generateDownloadURL() = %q, want prefix %q", got, want)
	}
}

// Cloud storage backends return a ready-to-use HTTP URL; it must be passed
// through untouched instead of being re-signed.
func TestGenerateDownloadURLKeepsHTTPFileServiceURL(t *testing.T) {
	t.Setenv("PUSH_FILES_EXTERNAL_URL", "")
	t.Setenv("APP_EXTERNAL_URL", "")

	const cloudURL = "https://bucket.example.com/manual.docx?X-Amz-Signature=abc"
	tool := newPushFilesTestTool(t, cloudURL)

	got, err := tool.generateDownloadURL(context.Background(), &types.Knowledge{
		ID: "k1", TenantID: 42, FilePath: "s3://bucket/manual.docx",
	}, 24)
	if err != nil {
		t.Fatalf("generateDownloadURL() error = %v", err)
	}
	if got != cloudURL {
		t.Fatalf("generateDownloadURL() = %q, want the backend URL %q unchanged", got, cloudURL)
	}
}

// stubPushFilesCatalog only needs CreateAccessGrant for these tests; the embedded
// interface surfaces accidental calls to other methods as panics.
type stubPushFilesCatalog struct {
	interfaces.ResourceCatalog
	grant    string
	grantErr error
}

func (s *stubPushFilesCatalog) CreateAccessGrant(context.Context, string, time.Duration) (string, error) {
	return s.grant, s.grantErr
}

func newPushFilesResourceTool(grant string, grantErr error) *PushFilesTool {
	return &PushFilesTool{
		catalog:  &stubPushFilesCatalog{grant: grant, grantErr: grantErr},
		tenantID: 42,
	}
}

// A resource:// handle must be minted as a /r/<token> capability URL built from
// the external base, with PUSH_FILES_EXTERNAL_URL taking priority over
// APP_EXTERNAL_URL. This is the canonical /r/ path the rest of the system uses.
func TestGenerateDownloadURLResourceHandleMintsResourceLink(t *testing.T) {
	t.Setenv("PUSH_FILES_EXTERNAL_URL", "https://dl.example.com")
	t.Setenv("APP_EXTERNAL_URL", "https://app.example.com")

	const handle = "resource://AbCdEfGhIjKlMnOpQrStUv"
	got, err := newPushFilesResourceTool("tokAbCdEf", nil).generateDownloadURL(
		context.Background(), &types.Knowledge{ID: "k1", TenantID: 42, FilePath: handle}, 24)
	if err != nil {
		t.Fatalf("generateDownloadURL() error = %v", err)
	}
	// PUSH_FILES_EXTERNAL_URL wins, and the link is a /r/ capability URL.
	if want := "https://dl.example.com/r/tokAbCdEf"; got != want {
		t.Fatalf("generateDownloadURL() = %q, want %q", got, want)
	}
}

// Without an externally reachable base, a resource:// handle cannot be exposed;
// the call must fail loudly rather than leaking the raw handle.
func TestGenerateDownloadURLResourceHandleRequiresExternalURL(t *testing.T) {
	t.Setenv("PUSH_FILES_EXTERNAL_URL", "")
	t.Setenv("APP_EXTERNAL_URL", "")

	_, err := newPushFilesResourceTool("tokAbCdEf", nil).generateDownloadURL(
		context.Background(),
		&types.Knowledge{ID: "k1", TenantID: 42, FilePath: "resource://AbCdEfGhIjKlMnOpQrStUv"},
		24,
	)
	if err == nil {
		t.Fatal("generateDownloadURL() error = nil, want an error requiring the external URL")
	}
}

// If the catalog cannot resolve/mint a grant for the handle, surface the real
// error instead of falling back to a signed raw handle (which would 404).
func TestGenerateDownloadURLResourceGrantFailure(t *testing.T) {
	t.Setenv("PUSH_FILES_EXTERNAL_URL", "https://dl.example.com")
	t.Setenv("APP_EXTERNAL_URL", "")

	_, err := newPushFilesResourceTool("", errors.New("resource not found")).generateDownloadURL(
		context.Background(),
		&types.Knowledge{ID: "k1", TenantID: 42, FilePath: "resource://AbCdEfGhIjKlMnOpQrStUv"},
		24,
	)
	if err == nil {
		t.Fatal("generateDownloadURL() error = nil, want the catalog grant error")
	}
}
