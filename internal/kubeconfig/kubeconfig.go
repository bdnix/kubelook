// Package kubeconfig parses an uploaded kubeconfig and builds REST configurations from it.
// Only self-contained credentials are accepted: everything a connection needs must be inside
// the file as data, because Kubelook cannot read the user's files and runs no user-supplied programs.
package kubeconfig

import (
	"encoding/base64"
	"fmt"
	"sort"
	"strings"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/bdnix/kubelook/internal/apperr"
)

const (
	// MaxBytes is the largest accepted kubeconfig.
	MaxBytes = 1 << 20
	// MaxLabelLength is the longest accepted label.
	MaxLabelLength = 63

	oidcProvider = "oidc"
)

// AuthKind says how a context's user authenticates.
type AuthKind string

// The supported ways to authenticate.
const (
	Certificate AuthKind = "certificate"
	Token       AuthKind = "token"
	OIDC        AuthKind = "oidc"
	None        AuthKind = "none"
)

// ContextInfo describes one usable context.
type ContextInfo struct {
	ID      string // "k:<label>/<context>"
	Name    string // the cluster entry's name
	Context string
	User    string
	Server  string
	Auth    AuthKind
}

// File is a validated kubeconfig held in memory.
type File struct {
	Label    string
	Contexts []ContextInfo // sorted by context name
	raw      *clientcmdapi.Config
}

// UnsupportedError says which entry of the kubeconfig cannot be used, and why.
type UnsupportedError struct {
	Kind   string // "user" | "cluster"
	Entry  string
	Reason string
}

func (e *UnsupportedError) Error() string {
	return fmt.Sprintf("kubeconfig %s %q: %s", e.Kind, e.Entry, e.Reason)
}

// Is makes the error match apperr.ErrInvalid, so it is answered as a bad request.
func (e *UnsupportedError) Is(target error) bool { return target == apperr.ErrInvalid }

func pathError(kind, entry, field string) error {
	return &UnsupportedError{Kind: kind, Entry: entry, Reason: fmt.Sprintf("file path references are not supported (%s): embed the data in the file instead", field)}
}

// ValidateLabel checks a kubeconfig label: 1 to 63 characters, no "/".
func ValidateLabel(label string) error {
	if label == "" || len(label) > MaxLabelLength || strings.Contains(label, "/") {
		return fmt.Errorf("label must be 1 to %d characters without \"/\": %w", MaxLabelLength, apperr.ErrInvalid)
	}
	return nil
}

// Parse validates content and returns the File. The whole file is rejected when any user,
// cluster or context in it is unsupported: there is no partial load.
func Parse(label string, content []byte) (*File, error) {
	if err := ValidateLabel(label); err != nil {
		return nil, err
	}
	if len(content) > MaxBytes {
		return nil, fmt.Errorf("kubeconfig is larger than %d bytes: %w", MaxBytes, apperr.ErrTooLarge)
	}
	raw, err := clientcmd.Load(content)
	if err != nil {
		return nil, fmt.Errorf("not a valid kubeconfig: %w: %w", apperr.ErrInvalid, err)
	}
	if len(raw.Contexts) == 0 {
		return nil, fmt.Errorf("kubeconfig has no contexts: %w", apperr.ErrInvalid)
	}
	kinds := map[string]AuthKind{}
	for _, name := range sortedKeys(raw.AuthInfos) {
		kind, err := checkUser(name, raw.AuthInfos[name])
		if err != nil {
			return nil, err
		}
		kinds[name] = kind
	}
	for _, name := range sortedKeys(raw.Clusters) {
		if raw.Clusters[name].CertificateAuthority != "" {
			return nil, pathError("cluster", name, "certificate-authority")
		}
	}
	f := &File{Label: label, raw: raw}
	for _, name := range sortedKeys(raw.Contexts) {
		ctx := raw.Contexts[name]
		cl, ok := raw.Clusters[ctx.Cluster]
		if !ok {
			return nil, fmt.Errorf("context %q refers to cluster %q, which is not in the file: %w", name, ctx.Cluster, apperr.ErrInvalid)
		}
		if _, ok := raw.AuthInfos[ctx.AuthInfo]; !ok {
			return nil, fmt.Errorf("context %q refers to user %q, which is not in the file: %w", name, ctx.AuthInfo, apperr.ErrInvalid)
		}
		f.Contexts = append(f.Contexts, ContextInfo{
			ID: "k:" + label + "/" + name, Name: ctx.Cluster, Context: name, User: ctx.AuthInfo,
			Server: cl.Server, Auth: kinds[ctx.AuthInfo],
		})
	}
	return f, nil
}

// checkUser accepts a user whose credentials are all inline data and says how it authenticates.
func checkUser(name string, u *clientcmdapi.AuthInfo) (AuthKind, error) {
	if u.Exec != nil {
		return "", &UnsupportedError{Kind: "user", Entry: name, Reason: "exec credential plugins are not supported"}
	}
	for _, p := range []struct{ field, value string }{
		{"client-certificate", u.ClientCertificate},
		{"client-key", u.ClientKey},
		{"tokenFile", u.TokenFile},
	} {
		if p.value != "" {
			return "", pathError("user", name, p.field)
		}
	}
	if u.Username != "" || u.Password != "" {
		return "", &UnsupportedError{Kind: "user", Entry: name, Reason: "username/password authentication is not supported"}
	}
	if u.AuthProvider != nil {
		return checkOIDC(name, u.AuthProvider)
	}
	switch {
	case u.Token != "":
		return Token, nil
	case len(u.ClientCertificateData) > 0:
		return Certificate, nil
	}
	return None, nil
}

func checkOIDC(name string, p *clientcmdapi.AuthProviderConfig) (AuthKind, error) {
	if p.Name != oidcProvider {
		return "", &UnsupportedError{Kind: "user", Entry: name, Reason: fmt.Sprintf("auth-provider %q is not supported (only oidc)", p.Name)}
	}
	if p.Config["idp-certificate-authority"] != "" {
		return "", pathError("user", name, "idp-certificate-authority")
	}
	if p.Config["idp-issuer-url"] == "" || p.Config["client-id"] == "" {
		return "", &UnsupportedError{Kind: "user", Entry: name, Reason: "the oidc auth-provider needs idp-issuer-url and client-id"}
	}
	return OIDC, nil
}

// RESTConfig builds a client configuration for one context from the in-memory data only. It
// never reads files or the environment. For OIDC users the current id-token is used as the bearer
// token; refreshing it is Kubelook's own job (not client-go's auth-provider plugin).
func (f *File) RESTConfig(context string) (*rest.Config, error) {
	if _, ok := f.raw.Contexts[context]; !ok {
		return nil, fmt.Errorf("context %q: %w", context, apperr.ErrNotFound)
	}
	cfg, err := clientcmd.NewNonInteractiveClientConfig(*f.raw, context, &clientcmd.ConfigOverrides{}, nil).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("building client config for context %q: %w", context, err)
	}
	if p := cfg.AuthProvider; p != nil {
		cfg.AuthProvider = nil
		if cfg.BearerToken == "" {
			cfg.BearerToken = p.Config["id-token"]
		}
	}
	cfg.ExecProvider = nil
	return cfg, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// OIDCSettings are the parameters of a context's oidc auth-provider.
type OIDCSettings struct {
	IssuerURL, ClientID, ClientSecret string
	RefreshToken, IDToken             string
	IDPCAData                         []byte // PEM; empty → system roots
}

// OIDC returns the oidc settings of the context's user; ok is false for any other kind of user.
func (f *File) OIDC(context string) (s OIDCSettings, ok bool) {
	ai := f.authInfo(context)
	if ai == nil || ai.AuthProvider == nil || ai.AuthProvider.Name != oidcProvider {
		return OIDCSettings{}, false
	}
	c := ai.AuthProvider.Config
	s = OIDCSettings{
		IssuerURL: c["idp-issuer-url"], ClientID: c["client-id"], ClientSecret: c["client-secret"],
		RefreshToken: c["refresh-token"], IDToken: c["id-token"],
	}
	if data := c["idp-certificate-authority-data"]; data != "" {
		if pem, err := base64.StdEncoding.DecodeString(data); err == nil {
			s.IDPCAData = pem
		}
	}
	return s, true
}

// SetOIDCTokens stores new tokens for the context's user (an empty refreshToken keeps the old one)
// and returns the serialized kubeconfig.
func (f *File) SetOIDCTokens(context, idToken, refreshToken string) ([]byte, error) {
	ai := f.authInfo(context)
	if ai == nil || ai.AuthProvider == nil {
		return nil, fmt.Errorf("context %q has no oidc user: %w", context, apperr.ErrNotFound)
	}
	if ai.AuthProvider.Config == nil {
		ai.AuthProvider.Config = map[string]string{}
	}
	ai.AuthProvider.Config["id-token"] = idToken
	if refreshToken != "" {
		ai.AuthProvider.Config["refresh-token"] = refreshToken
	}
	out, err := clientcmd.Write(*f.raw)
	if err != nil {
		return nil, fmt.Errorf("serializing kubeconfig: %w", err)
	}
	return out, nil
}

func (f *File) authInfo(context string) *clientcmdapi.AuthInfo {
	cx, ok := f.raw.Contexts[context]
	if !ok {
		return nil
	}
	return f.raw.AuthInfos[cx.AuthInfo]
}
