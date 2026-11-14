package kube

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sync"
	"time"

	"k8s.io/client-go/rest"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/session"
)

// OpenAPI limits.
const (
	openAPITTL = 5 * time.Minute
	// MaxOpenAPIBytes bounds one OpenAPI document (the core v1 document is a few MiB).
	MaxOpenAPIBytes = 32 << 20
	openAPITimeout  = 30 * time.Second
)

var openAPIName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]*$`)

type openAPIEntry struct {
	body    []byte
	fetched time.Time
}

// OpenAPI reads a cluster's OpenAPI v3 documents with the user's own credentials and keeps them for a
// few minutes per session and cluster.
type OpenAPI struct {
	clients Clients
	clock   clock.Clock
	mu      sync.Mutex
	cache   map[string]openAPIEntry
}

// NewOpenAPI returns the OpenAPI reader.
func NewOpenAPI(c Clients, clk clock.Clock) *OpenAPI {
	return &OpenAPI{clients: c, clock: clk, cache: map[string]openAPIEntry{}}
}

func openAPIPath(group, version string) (string, error) {
	if !openAPIName.MatchString(group) || !openAPIName.MatchString(version) {
		return "", fmt.Errorf("group and version must be plain names: %w", apperr.ErrInvalid)
	}
	if group == "core" {
		if version != "v1" {
			return "", fmt.Errorf("the core group has only version v1: %w", apperr.ErrInvalid)
		}
		return "/openapi/v3/api/v1", nil
	}
	return "/openapi/v3/apis/" + group + "/" + version, nil
}

// Get returns the OpenAPI v3 document of the group and version ("core" is the empty group).
func (o *OpenAPI) Get(ctx context.Context, sess session.Session, clusterID, group, version string) ([]byte, error) {
	path, err := openAPIPath(group, version)
	if err != nil {
		return nil, err
	}
	key := sess.Hash + "\x00" + clusterID + "\x00" + path
	now := o.clock.Now()
	o.mu.Lock()
	if e, ok := o.cache[key]; ok && now.Sub(e.fetched) < openAPITTL {
		o.mu.Unlock()
		return e.body, nil
	}
	o.mu.Unlock()

	cfg, err := o.clients.REST(ctx, sess, clusterID)
	if err != nil {
		return nil, err
	}
	body, err := fetchOpenAPI(ctx, cfg, path)
	if err != nil {
		return nil, err
	}
	o.mu.Lock()
	o.cache[key] = openAPIEntry{body: body, fetched: now}
	o.mu.Unlock()
	return body, nil
}

func fetchOpenAPI(ctx context.Context, cfg *rest.Config, path string) ([]byte, error) {
	hc, err := rest.HTTPClientFor(cfg)
	if err != nil {
		return nil, fmt.Errorf("building the HTTP client: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, openAPITimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.Host+path, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("building the request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reading the OpenAPI document: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, fmt.Errorf("the cluster serves no OpenAPI document for this group and version: %w", apperr.ErrNotFound)
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, fmt.Errorf("you may not read the OpenAPI document: %w", apperr.ErrForbidden)
	default:
		return nil, fmt.Errorf("the cluster answered %d for the OpenAPI document", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxOpenAPIBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading the OpenAPI document: %w", err)
	}
	if len(body) > MaxOpenAPIBytes {
		return nil, fmt.Errorf("the OpenAPI document exceeds %d bytes: %w", MaxOpenAPIBytes, apperr.ErrTooLarge)
	}
	return body, nil
}
