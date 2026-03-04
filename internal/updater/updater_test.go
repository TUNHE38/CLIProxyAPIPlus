package updater

import (
    "archive/tar"
    "bytes"
    "fmt"
    "compress/gzip"
    "io"
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "runtime"
    "strings"
    "testing"
)

// makeTestArchive creates an in-memory tar.gz containing a single file named
// "cli-proxy-api-plus" with the provided body.
func makeTestArchive(body string) ([]byte, error) {
    var buf bytes.Buffer
    gz := gzip.NewWriter(&buf)
    tw := tar.NewWriter(gz)

    hdr := &tar.Header{
        Name: "cli-proxy-api-plus",
        Mode: 0755,
        Size: int64(len(body)),
    }
    if err := tw.WriteHeader(hdr); err != nil {
        return nil, err
    }
    if _, err := tw.Write([]byte(body)); err != nil {
        return nil, err
    }
    if err := tw.Close(); err != nil {
        return nil, err
    }
    if err := gz.Close(); err != nil {
        return nil, err
    }
    return buf.Bytes(), nil
}

func TestExtractBinary(t *testing.T) {
    data, err := makeTestArchive("hello")
    if err != nil {
        t.Fatal(err)
    }
    tmp := t.TempDir()
    arch := filepath.Join(tmp, "test.tar.gz")
    if err := os.WriteFile(arch, data, 0644); err != nil {
        t.Fatal(err)
    }
    out, err := extractBinary(arch, "cli-proxy-api-plus")
    if err != nil {
        t.Fatalf("extract failed: %v", err)
    }
    got, err := os.ReadFile(out)
    if err != nil {
        t.Fatal(err)
    }
    if string(got) != "hello" {
        t.Errorf("unexpected content: %q", string(got))
    }
}

func TestSelfUpdate_NoAsset(t *testing.T) {
    // serve a release without matching asset
    ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        io.WriteString(w, `{"tag_name":"v1.0.0","assets":[]}`)
    }))
    defer ts.Close()

    // override url by temporarily replacing the function that builds it?
    // easiest is to stub http.Get via default client using Transport
    orig := http.DefaultTransport
    http.DefaultTransport = &transport{base: orig, url: ts.URL}
    defer func() { http.DefaultTransport = orig }()

    err := SelfUpdate("owner", "repo")
    if err == nil || !strings.Contains(err.Error(), "does not contain asset") {
        t.Fatalf("expected missing asset error, got %v", err)
    }
}

// transport helps redirect the release request to our test server
// and later asset requests as well.

type transport struct {
    base http.RoundTripper
    url  string
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
    // rewrite any host to the test server
    req.URL.Scheme = "http"
    req.URL.Host = strings.TrimPrefix(t.url, "http://")
    return t.base.RoundTrip(req)
}

func TestSelfUpdate_Success(t *testing.T) {
    version := "1.2.3"
    assetName := fmt.Sprintf("cli-proxy-api-plus_%s_%s_%s.tar.gz", version, runtime.GOOS, runtime.GOARCH)
    archiveData, err := makeTestArchive("world")
    if err != nil {
        t.Fatal(err)
    }

    // we need to declare ts before using it in the handler closure
    var ts *httptest.Server
    ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        switch r.URL.Path {
        case "/repos/owner/repo/releases/latest":
            w.Header().Set("Content-Type", "application/json")
            fmt.Fprintf(w, `{"tag_name":"v%s","assets":[{"name":"%s","browser_download_url":"%s/download"}]}`, version, assetName, ts.URL)
        case "/download":
            w.Write(archiveData)
        default:
            t.Fatalf("unexpected path %s", r.URL.Path)
        }
    }))
    defer ts.Close()

    orig := http.DefaultTransport
    http.DefaultTransport = &transport{base: orig, url: ts.URL}
    defer func() { http.DefaultTransport = orig }()

    // run self update, expect no error; resulting binary file should be
    // replaced in current dir but since we cannot modify os.Executable location
    // easily in test, we simply make sure it doesn't error when finding asset
    err = SelfUpdate("owner", "repo")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
}
