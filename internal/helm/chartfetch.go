package helm

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Masterminds/semver/v3"
	"sigs.k8s.io/yaml"

	"github.com/bdnix/kubelook/internal/apperr"
)

// maxIndexBytes bounds the repository index a download reads.
const maxIndexBytes = 32 << 20

var chartName = regexp.MustCompile(`^[a-z0-9]([-a-z0-9._]*[a-z0-9])?$`)

// Repository is a chart repository with the credentials of its entry.
type Repository struct {
	Name, URL, Username, Password string
}

// Fetcher downloads charts from HTTP repositories. It writes only into the directory it is given.
type Fetcher struct {
	HTTP *http.Client
	// MaxBytes is `helm.maxChartBytes`.
	MaxBytes int64
}

type index struct {
	Entries map[string][]struct {
		Version string   `json:"version"`
		URLs    []string `json:"urls"`
	} `json:"entries"`
}

func (f *Fetcher) client() *http.Client {
	if f.HTTP != nil {
		return f.HTTP
	}
	return http.DefaultClient
}

func (f *Fetcher) get(ctx context.Context, repo Repository, target *url.URL) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), http.NoBody)
	if err != nil {
		return nil, err
	}
	base, _ := url.Parse(repo.URL)
	// credentials go only to the host of the repository itself
	if repo.Username != "" && base != nil && base.Host == target.Host {
		req.SetBasicAuth(repo.Username, repo.Password)
	}
	resp, err := f.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("contacting the repository: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("the repository answered %d for %s", resp.StatusCode, target.Path)
	}
	return resp, nil
}

// pick chooses the version asked for, or the newest release (a pre-release only when nothing else exists).
func pick(versions []string, want string) (string, bool) {
	var best *semver.Version
	var bestRaw string
	var bestPre *semver.Version
	var bestPreRaw string
	for _, raw := range versions {
		if want != "" {
			if raw == want {
				return raw, true
			}
			continue
		}
		v, err := semver.NewVersion(raw)
		if err != nil {
			continue
		}
		if v.Prerelease() != "" {
			if bestPre == nil || v.GreaterThan(bestPre) {
				bestPre, bestPreRaw = v, raw
			}
			continue
		}
		if best == nil || v.GreaterThan(best) {
			best, bestRaw = v, raw
		}
	}
	switch {
	case want != "":
		return "", false
	case best != nil:
		return bestRaw, true
	case bestPre != nil:
		return bestPreRaw, true
	}
	return "", false
}

// Fetch downloads the chart into destDir and returns the path of the archive. A chart larger than MaxBytes is
// refused with apperr.ErrTooLarge and the partial file is removed.
func (f *Fetcher) Fetch(ctx context.Context, repo Repository, name, version, destDir string) (string, error) {
	if !chartName.MatchString(name) {
		return "", fmt.Errorf("%q is not a chart name: %w", name, apperr.ErrInvalid)
	}
	base, err := url.Parse(strings.TrimRight(repo.URL, "/") + "/")
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return "", fmt.Errorf("the repository address must be an http or https URL: %w", apperr.ErrInvalid)
	}
	resp, err := f.get(ctx, repo, base.JoinPath("index.yaml"))
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxIndexBytes))
	if err != nil {
		return "", fmt.Errorf("reading the repository index: %w", err)
	}
	var idx index
	if err := yaml.Unmarshal(raw, &idx); err != nil {
		return "", fmt.Errorf("the repository index is not valid: %w", apperr.ErrInvalid)
	}
	entries := idx.Entries[name]
	versions := make([]string, len(entries))
	for i, e := range entries {
		versions[i] = e.Version
	}
	chosen, ok := pick(versions, version)
	if !ok {
		return "", fmt.Errorf("the repository has no chart %s %s: %w", name, version, apperr.ErrNotFound)
	}
	var link string
	for _, e := range entries {
		if e.Version == chosen && len(e.URLs) > 0 {
			link = e.URLs[0]
		}
	}
	chartURL, err := base.Parse(link)
	if err != nil || link == "" || (chartURL.Scheme != "http" && chartURL.Scheme != "https") {
		return "", fmt.Errorf("the chart address in the index is not valid: %w", apperr.ErrInvalid)
	}
	return f.download(ctx, repo, chartURL, destDir)
}

func (f *Fetcher) download(ctx context.Context, repo Repository, chartURL *url.URL, destDir string) (_ string, err error) {
	resp, err := f.get(ctx, repo, chartURL)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	path := filepath.Join(destDir, "chart.tgz")
	out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600) //nolint:gosec // the path is a fixed name inside the job's own directory
	if err != nil {
		return "", fmt.Errorf("creating the chart file: %w", err)
	}
	defer func() {
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			_ = os.Remove(path)
		}
	}()
	n, err := io.Copy(out, io.LimitReader(resp.Body, f.MaxBytes+1))
	if err != nil {
		return "", fmt.Errorf("downloading the chart: %w", err)
	}
	if n > f.MaxBytes {
		return "", fmt.Errorf("the chart is larger than %d bytes: %w", f.MaxBytes, apperr.ErrTooLarge)
	}
	return path, nil
}
