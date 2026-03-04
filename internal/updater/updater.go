package updater

import (
    "archive/tar"
    "compress/gzip"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "os"
    "path/filepath"
    "runtime"
    "strings"
)

// release represents the structure returned by GitHub's releases/latest API
// for the fields we care about.  Only the tag name and asset download
// URLs are parsed.
type release struct {
    TagName string `json:"tag_name"`
    Assets  []struct {
        Name               string `json:"name"`
        BrowserDownloadURL string `json:"browser_download_url"`
    } `json:"assets"`
}

// SelfUpdate fetches the latest release for the given owner/repo, downloads
// the archive matching the current GOOS/GOARCH, and replaces the running
// executable with the new binary.  It returns an error if anything fails.
func SelfUpdate(owner, repo string) error {
    url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)
    resp, err := http.Get(url)
    if err != nil {
        return fmt.Errorf("failed to query latest release: %w", err)
    }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("unexpected status from release endpoint: %s", resp.Status)
    }
    var r release
    if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
        return fmt.Errorf("unable to decode release JSON: %w", err)
    }

    version := strings.TrimPrefix(r.TagName, "v")
    assetName := fmt.Sprintf("cli-proxy-api-plus_%s_%s_%s.tar.gz", version, runtime.GOOS, runtime.GOARCH)

    var assetURL string
    for _, a := range r.Assets {
        if a.Name == assetName {
            assetURL = a.BrowserDownloadURL
            break
        }
    }
    if assetURL == "" {
        return fmt.Errorf("release %s does not contain asset %s", r.TagName, assetName)
    }

    tmp, err := os.CreateTemp("", "cpa-update-*.tar.gz")
    if err != nil {
        return fmt.Errorf("unable to create temp file: %w", err)
    }
    defer os.Remove(tmp.Name())
    defer tmp.Close()

    dl, err := http.Get(assetURL)
    if err != nil {
        return fmt.Errorf("download failed: %w", err)
    }
    defer dl.Body.Close()
    if dl.StatusCode != http.StatusOK {
        return fmt.Errorf("download returned %s", dl.Status)
    }

    if _, err := io.Copy(tmp, dl.Body); err != nil {
        return fmt.Errorf("writing to temp file: %w", err)
    }

    // extract the binary from the archive into a temporary file
    binPath, err := extractBinary(tmp.Name(), "cli-proxy-api-plus")
    if err != nil {
        return fmt.Errorf("failed to unpack archive: %w", err)
    }
    defer os.Remove(binPath)

    exePath, err := os.Executable()
    if err != nil {
        return fmt.Errorf("cannot determine executable path: %w", err)
    }

    // on unix we can overwrite a running binary; on windows this will fail
    // so we attempt to copy instead of rename if necessary
    if err := os.Rename(binPath, exePath); err != nil {
        // fallback to copy
        if err := copyFile(binPath, exePath); err != nil {
            return fmt.Errorf("unable to replace executable: %w", err)
        }
    }

    return nil
}

// extractBinary locates the specified file inside a tar.gz archive and
// writes it to a new temporary file with execute permissions.  It returns the
// path of the extracted file.
func extractBinary(archive, name string) (string, error) {
    f, err := os.Open(archive)
    if err != nil {
        return "", err
    }
    defer f.Close()

    gz, err := gzip.NewReader(f)
    if err != nil {
        return "", err
    }
    defer gz.Close()

    tr := tar.NewReader(gz)
    for {
        hdr, err := tr.Next()
        if err == io.EOF {
            break
        }
        if err != nil {
            return "", err
        }
        // tar headers sometimes include ./ prefix
        if filepath.Base(hdr.Name) == name {
            out, err := os.CreateTemp("", name+"-")
            if err != nil {
                return "", err
            }
            if _, err := io.Copy(out, tr); err != nil {
                out.Close()
                return "", err
            }
            out.Close()
            if err := os.Chmod(out.Name(), 0755); err != nil {
                return "", err
            }
            return out.Name(), nil
        }
    }
    return "", fmt.Errorf("binary %s not found in archive", name)
}

// simple copy function used as a fallback on systems where renaming in-use
// executables is not permitted.
func copyFile(src, dst string) error {
    in, err := os.Open(src)
    if err != nil {
        return err
    }
    defer in.Close()
    out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
    if err != nil {
        return err
    }
    defer out.Close()
    _, err = io.Copy(out, in)
    return err
}
