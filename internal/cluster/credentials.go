// Package cluster holds what a signed-in user brings to reach clusters: the Vault token and,
// later, kubeconfigs and cluster credentials.
package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/config"
	"github.com/bdnix/kubelook/internal/crypto"
	"github.com/bdnix/kubelook/internal/kubeconfig"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/store"
	"github.com/bdnix/kubelook/internal/vault"
)

// Kinds of session credentials and of browser copies.
const (
	kindVaultToken  = "vault_token"
	kindKubeconfig  = "kubeconfig"
	kindClusterCred = "cluster_cred"
	kindClusterRole = "cluster_role"

	// vaultClusterPrefix starts the ID of every cluster that comes from Vault.
	vaultClusterPrefix = "v:"
	// kubeconfigClusterPrefix starts the ID of every cluster that comes from an uploaded kubeconfig: "k:<label>/<context>".
	kubeconfigClusterPrefix = "k:"
)

// Restore result statuses.
const (
	StatusOK               = "ok"
	StatusKeyChanged       = "key_changed"
	StatusWrongUser        = "wrong_user"
	StatusExpired          = "expired"
	StatusInvalid          = "invalid"
	StatusVaultUnreachable = "vault_unreachable"
)

// KeyChangedMessage is what the user is told when the encryption key changed (spec, Sessions).
const KeyChangedMessage = "Kubelook's encryption key was changed, so your saved Vault token and kubeconfigs can't be used anymore. " +
	"Please paste your token and upload your kubeconfigs again."

// Ender ends a session (the session manager).
type Ender interface {
	End(ctx context.Context, hash string) error
}

// Deps are the dependencies of Credentials.
type Deps struct {
	Sessions *store.Sessions
	Sealer   *crypto.Sealer
	Vault    vault.API
	Clock    clock.Clock
	Config   *config.Config
	Audit    *audit.Recorder
	Registry *session.Registry
	Ender    Ender
	Log      *slog.Logger
	Cache    *DiscoveryCache // dropped when the Vault token changes; nil → none
}

// Credentials manages the credentials of one signed-in user's sessions.
type Credentials struct{ d Deps }

// NewCredentials returns the credential manager.
func NewCredentials(d Deps) *Credentials { return &Credentials{d: d} }

// VaultStatus describes the Vault token for the UI.
type VaultStatus struct {
	DisplayName  string     `json:"displayName"`
	EntityID     string     `json:"entityId"`
	ExpiresAt    *time.Time `json:"expiresAt"`
	TTLSeconds   int        `json:"ttlSeconds"`
	Renewable    bool       `json:"renewable"`
	CanRenew     bool       `json:"canRenew"`
	NeverExpires bool       `json:"neverExpires"`
	Warning      bool       `json:"warning"`
	Expired      bool       `json:"expired"`
}

// Info describes a cluster a credential reaches. Vault clusters carry Roles; kubeconfig
// clusters carry Context, User and Server.
type Info struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Context string   `json:"context,omitempty"`
	User    string   `json:"user,omitempty"`
	Server  string   `json:"server,omitempty"`
	Roles   []string `json:"roles,omitempty"`
}

// RestoreResult is the outcome for one browser copy.
type RestoreResult struct {
	Status   string       `json:"status"`
	Kind     string       `json:"kind,omitempty"`
	Label    string       `json:"label,omitempty"`
	Vault    *VaultStatus `json:"vault,omitempty"`
	Clusters []Info       `json:"clusters,omitempty"`
	Message  string       `json:"message,omitempty"`
}

// vaultCred is the plaintext of the sealed vault_token session credential.
type vaultCred struct {
	Token       string `json:"token"`
	EntityID    string `json:"entityId"`
	DisplayName string `json:"displayName"`
	MaxReached  bool   `json:"maxReached"` // renewing no longer extends the TTL
}

// browserCopy is the plaintext of a sealed copy kept in the browser.
type browserCopy struct {
	Kind    string    `json:"kind"`
	Token   string    `json:"token,omitempty"`   // vault_token
	Label   string    `json:"label,omitempty"`   // kubeconfig
	Content string    `json:"content,omitempty"` // kubeconfig
	SavedAt time.Time `json:"savedAt"`
}

var (
	errNoVaultToken = fmt.Errorf("no Vault token in this session: %w", apperr.ErrNotFound)
	errTokenInvalid = &apperr.APIError{Status: http.StatusBadRequest, Code: "bad_request", Message: "Vault rejected this token."}
	errUnreachable  = &apperr.APIError{Status: http.StatusServiceUnavailable, Code: "vault_unreachable", Message: "Vault is unreachable."}
	errKeyChanged   = &apperr.APIError{Status: http.StatusUnauthorized, Code: "key_changed", Message: KeyChangedMessage}
)

func (c *Credentials) sealForBrowser(userID string, payload any) (string, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encoding browser copy: %w", err)
	}
	sealed, err := c.d.Sealer.Seal(b, crypto.AAD(crypto.PurposeSavedCredential, userID))
	if err != nil {
		return "", fmt.Errorf("sealing browser copy: %w", err)
	}
	return sealed, nil
}

func (c *Credentials) openFromBrowser(userID, sealed string) (browserCopy, error) {
	pt, err := c.d.Sealer.Open(sealed, crypto.AAD(crypto.PurposeSavedCredential, userID))
	if err != nil {
		return browserCopy{}, err
	}
	var bc browserCopy
	if err := json.Unmarshal(pt, &bc); err != nil {
		return browserCopy{}, fmt.Errorf("decoding browser copy: %w: %w", crypto.ErrMalformed, err)
	}
	return bc, nil
}

func (c *Credentials) putVaultCred(ctx context.Context, sess session.Session, vc vaultCred, expires time.Time) error {
	b, err := json.Marshal(vc)
	if err != nil {
		return fmt.Errorf("encoding vault credential: %w", err)
	}
	sealed, err := c.d.Sealer.Seal(b, crypto.AAD(crypto.PurposeSessionCredential, sess.Hash))
	if err != nil {
		return fmt.Errorf("sealing vault credential: %w", err)
	}
	return c.d.Sessions.PutCredential(ctx, sess.Hash,
		store.CredentialRow{Kind: kindVaultToken, Sealed: sealed, ExpiresAt: expires}, c.d.Clock.Now())
}

// openSessionCredential opens a sealed session credential. One sealed with another key ends the
// session and answers 401 key_changed.
func (c *Credentials) openSessionCredential(ctx context.Context, sess session.Session, sealed string) ([]byte, error) {
	pt, err := c.d.Sealer.Open(sealed, crypto.AAD(crypto.PurposeSessionCredential, sess.Hash))
	if errors.Is(err, crypto.ErrUnknownKey) {
		if eerr := c.d.Ender.End(ctx, sess.Hash); eerr != nil {
			c.d.Log.Warn("ending session after key change", "error", eerr.Error())
		}
		return nil, errKeyChanged
	}
	if err != nil {
		return nil, fmt.Errorf("opening session credential: %w", err)
	}
	return pt, nil
}

// getVaultCred reads the session's Vault credential.
func (c *Credentials) getVaultCred(ctx context.Context, sess session.Session) (vaultCred, error) {
	vc, _, err := c.loadVaultCred(ctx, sess)
	return vc, err
}

// loadVaultCred reads the session's Vault credential and the expiry stored with it (zero: none).
func (c *Credentials) loadVaultCred(ctx context.Context, sess session.Session) (vaultCred, time.Time, error) {
	row, err := c.d.Sessions.GetCredential(ctx, sess.Hash, kindVaultToken, "")
	if errors.Is(err, store.ErrNotFound) {
		return vaultCred{}, time.Time{}, errNoVaultToken
	}
	if err != nil {
		return vaultCred{}, time.Time{}, err
	}
	pt, err := c.openSessionCredential(ctx, sess, row.Sealed)
	if err != nil {
		return vaultCred{}, time.Time{}, err
	}
	var vc vaultCred
	if err := json.Unmarshal(pt, &vc); err != nil {
		return vaultCred{}, time.Time{}, fmt.Errorf("decoding vault credential: %w", err)
	}
	return vc, row.ExpiresAt, nil
}

// loadKubeconfigs returns the session's kubeconfigs sorted by label. One that no longer parses is skipped.
func (c *Credentials) loadKubeconfigs(ctx context.Context, sess session.Session) ([]*kubeconfig.File, error) {
	rows, err := c.d.Sessions.ListCredentials(ctx, sess.Hash, kindKubeconfig)
	if err != nil {
		return nil, err
	}
	files := make([]*kubeconfig.File, 0, len(rows))
	for _, r := range rows {
		pt, err := c.openSessionCredential(ctx, sess, r.Sealed)
		if err != nil {
			return nil, err
		}
		f, err := kubeconfig.Parse(r.Ref, pt)
		if err != nil {
			c.d.Log.Warn("skipping a stored kubeconfig that no longer parses", "label", r.Ref, "error", err.Error())
			continue
		}
		files = append(files, f)
	}
	return files, nil
}

func (c *Credentials) invalidateDiscovery(sess session.Session) {
	if c.d.Cache != nil {
		c.d.Cache.Invalidate(sess.Hash)
	}
}

// lookup asks Vault about the token and maps failures to API errors.
func (c *Credentials) lookup(ctx context.Context, token string) (vault.TokenInfo, error) {
	info, err := c.d.Vault.LookupSelf(ctx, token)
	switch {
	case errors.Is(err, vault.ErrPermissionDenied):
		return vault.TokenInfo{}, errTokenInvalid
	case errors.Is(err, vault.ErrUnreachable):
		return vault.TokenInfo{}, errUnreachable
	case err != nil:
		return vault.TokenInfo{}, fmt.Errorf("looking up vault token: %w", err)
	}
	return info, nil
}

// remaining is the time the token has left, from its expiry and the clock (not from the stale ttl number).
func (c *Credentials) remaining(info vault.TokenInfo) time.Duration {
	if info.NeverExpires() {
		return 0
	}
	if info.ExpireTime.IsZero() {
		return info.TTL
	}
	return max(info.ExpireTime.Sub(c.d.Clock.Now()), 0)
}

func (c *Credentials) status(info vault.TokenInfo, vc vaultCred) VaultStatus {
	left := c.remaining(info)
	st := VaultStatus{
		DisplayName: info.DisplayName, EntityID: info.EntityID, Renewable: info.Renewable,
		NeverExpires: info.NeverExpires(), TTLSeconds: int(left / time.Second),
	}
	if !info.NeverExpires() {
		exp := info.ExpireTime
		if exp.IsZero() {
			exp = c.d.Clock.Now().Add(info.TTL)
		}
		st.ExpiresAt = &exp
		st.Expired = left <= 0
		st.Warning = left > 0 && left < c.d.Config.Vault.ExpiryWarning.D()
		st.CanRenew = info.Renewable && info.Type != "batch" && !vc.MaxReached
	}
	return st
}

func (c *Credentials) storeVaultToken(ctx context.Context, sess session.Session, token string, info vault.TokenInfo) (VaultStatus, error) {
	vc := vaultCred{Token: token, EntityID: info.EntityID, DisplayName: info.DisplayName}
	if err := c.putVaultCred(ctx, sess, vc, c.expiry(info)); err != nil {
		return VaultStatus{}, err
	}
	c.invalidateDiscovery(sess)
	return c.status(info, vc), nil
}

func (c *Credentials) expiry(info vault.TokenInfo) time.Time {
	if info.NeverExpires() {
		return time.Time{}
	}
	if info.ExpireTime.IsZero() {
		return c.d.Clock.Now().Add(info.TTL)
	}
	return info.ExpireTime
}

// AddVaultToken validates the token, keeps it in the session and returns the sealed copy for the browser.
func (c *Credentials) AddVaultToken(ctx context.Context, sess session.Session, token string) (sealed string, st VaultStatus, err error) {
	info, err := c.lookup(ctx, token)
	if err != nil {
		return "", VaultStatus{}, err
	}
	if st, err = c.storeVaultToken(ctx, sess, token, info); err != nil {
		return "", VaultStatus{}, err
	}
	sealed, err = c.sealForBrowser(sess.UserID, browserCopy{Kind: kindVaultToken, Token: token, SavedAt: c.d.Clock.Now().UTC()})
	if err != nil {
		return "", VaultStatus{}, err
	}
	return sealed, st, nil
}

// Restore re-establishes credentials from the sealed copies the browser kept.
func (c *Credentials) Restore(ctx context.Context, sess session.Session, sealed []string) []RestoreResult {
	results := make([]RestoreResult, len(sealed))
	keyChanged := false
	for i, s := range sealed {
		bc, err := c.openFromBrowser(sess.UserID, s)
		switch {
		case errors.Is(err, crypto.ErrUnknownKey):
			if !keyChanged {
				keyChanged = true
				c.auditKeyChanged(ctx, s)
			}
			results[i] = RestoreResult{Status: StatusKeyChanged, Message: KeyChangedMessage}
		case errors.Is(err, crypto.ErrDecrypt):
			results[i] = RestoreResult{Status: StatusWrongUser}
		case err != nil:
			results[i] = RestoreResult{Status: StatusInvalid}
		case bc.Kind == kindVaultToken:
			results[i] = c.restoreVaultToken(ctx, sess, bc.Token)
		case bc.Kind == kindKubeconfig:
			results[i] = c.restoreKubeconfig(ctx, sess, bc)
		default:
			results[i] = RestoreResult{Status: StatusInvalid, Message: "Unsupported credential."}
		}
	}
	return results
}

func (c *Credentials) auditKeyChanged(ctx context.Context, sealed string) {
	saved, _ := crypto.KeyIDOf(sealed)
	err := c.d.Audit.Record(ctx, audit.Event{
		Verb: audit.VerbKeyChanged, Outcome: "failure",
		Details: map[string]any{"savedKeyId": saved, "currentKeyId": c.d.Sealer.KeyID()},
	})
	if err != nil {
		c.d.Log.Warn("recording key change", "error", err.Error())
	}
}

func (c *Credentials) restoreVaultToken(ctx context.Context, sess session.Session, token string) RestoreResult {
	res := RestoreResult{Kind: kindVaultToken}
	info, err := c.lookup(ctx, token)
	switch {
	case errors.Is(err, errTokenInvalid):
		res.Status, res.Message = StatusExpired, "Your saved Vault token has expired. Please paste a new token."
		return res
	case errors.Is(err, errUnreachable):
		// Keep the token so discovery can be retried once Vault is back.
		if perr := c.putVaultCred(ctx, sess, vaultCred{Token: token}, time.Time{}); perr != nil {
			c.d.Log.Warn("keeping vault token", "error", perr.Error())
		}
		res.Status = StatusVaultUnreachable
		return res
	case err != nil:
		c.d.Log.Warn("restoring vault token", "error", err.Error())
		res.Status = StatusInvalid
		return res
	}
	st, err := c.storeVaultToken(ctx, sess, token, info)
	if err != nil {
		c.d.Log.Warn("storing vault token", "error", err.Error())
		res.Status = StatusInvalid
		return res
	}
	res.Label, res.Vault = info.DisplayName, &st
	clusters, err := vault.Discover(ctx, c.d.Vault, token, c.d.Config.Vault.KubernetesMountPrefix, c.d.Config.Vault.CandidateRoles)
	if errors.Is(err, vault.ErrUnreachable) {
		res.Status = StatusVaultUnreachable
		return res
	}
	if err != nil {
		c.d.Log.Warn("discovering clusters", "error", err.Error())
		res.Status = StatusInvalid
		return res
	}
	res.Status = StatusOK
	for _, cl := range clusters {
		res.Clusters = append(res.Clusters, Info{ID: vaultClusterPrefix + cl.Name, Name: cl.Name, Roles: cl.Roles})
	}
	return res
}

// RenewVaultToken renews the token. It is only ever called because the user asked for it.
func (c *Credentials) RenewVaultToken(ctx context.Context, sess session.Session) (VaultStatus, error) {
	vc, err := c.getVaultCred(ctx, sess)
	if err != nil {
		return VaultStatus{}, err
	}
	info, err := c.lookup(ctx, vc.Token)
	if errors.Is(err, errTokenInvalid) {
		return VaultStatus{}, fmt.Errorf("this Vault token has expired; paste a new one: %w", apperr.ErrConflict)
	}
	if err != nil {
		return VaultStatus{}, err
	}
	if !info.Renewable || info.Type == "batch" || info.NeverExpires() {
		return VaultStatus{}, fmt.Errorf("this Vault token cannot be renewed; get a new one from Vault: %w", apperr.ErrConflict)
	}
	before := c.remaining(info)
	newTTL, err := c.d.Vault.RenewSelf(ctx, vc.Token)
	if err != nil {
		return VaultStatus{}, fmt.Errorf("renewing vault token: %w", err)
	}
	if newTTL <= before+time.Second {
		vc.MaxReached = true
	}
	if info, err = c.lookup(ctx, vc.Token); err != nil {
		return VaultStatus{}, err
	}
	if err := c.putVaultCred(ctx, sess, vc, c.expiry(info)); err != nil {
		return VaultStatus{}, err
	}
	c.invalidateDiscovery(sess)
	return c.status(info, vc), nil
}

// RemoveVaultToken deletes the token and everything that came from it: the credentials of Vault
// clusters, and the open streams of those clusters.
func (c *Credentials) RemoveVaultToken(ctx context.Context, sess session.Session) error {
	c.invalidateDiscovery(sess)
	if err := c.d.Sessions.DeleteCredential(ctx, sess.Hash, kindVaultToken, ""); err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	for _, kind := range []string{kindClusterCred, kindClusterRole} {
		rows, err := c.d.Sessions.ListCredentials(ctx, sess.Hash, kind)
		if err != nil {
			return err
		}
		for _, r := range rows {
			if !strings.HasPrefix(r.Ref, vaultClusterPrefix) {
				continue
			}
			if err := c.d.Sessions.DeleteCredential(ctx, sess.Hash, kind, r.Ref); err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
		}
	}
	return c.d.Registry.CloseMatching(sess.Hash, func(name string) bool { return strings.HasPrefix(name, vaultClusterPrefix) })
}

// VaultToken returns the session's Vault token and what Vault says about it (for later steps).
func (c *Credentials) VaultToken(ctx context.Context, sess session.Session) (string, vault.TokenInfo, error) {
	vc, err := c.getVaultCred(ctx, sess)
	if err != nil {
		return "", vault.TokenInfo{}, err
	}
	info, err := c.d.Vault.LookupSelf(ctx, vc.Token)
	if err != nil {
		return "", vault.TokenInfo{}, fmt.Errorf("looking up vault token: %w", err)
	}
	return vc.Token, info, nil
}

// VaultStatus describes the session's Vault token, or returns nil when there is none.
// A token Vault no longer accepts is reported as expired.
func (c *Credentials) VaultStatus(ctx context.Context, sess session.Session) (*VaultStatus, error) {
	vc, err := c.getVaultCred(ctx, sess)
	if errors.Is(err, errNoVaultToken) {
		return nil, nil //nolint:nilnil // no token is a normal answer, not an error
	}
	if err != nil {
		return nil, err
	}
	info, err := c.lookup(ctx, vc.Token)
	if errors.Is(err, errTokenInvalid) {
		return &VaultStatus{DisplayName: vc.DisplayName, EntityID: vc.EntityID, Expired: true}, nil
	}
	if err != nil {
		return nil, err
	}
	st := c.status(info, vc)
	return &st, nil
}

// VaultIdentity returns the Vault entity ID and display name stored with the token, for audit
// records. Both are empty when the session has no Vault token.
func (c *Credentials) VaultIdentity(ctx context.Context, sess session.Session) (entityID, displayName string) {
	vc, err := c.getVaultCred(ctx, sess)
	if err != nil {
		return "", ""
	}
	return vc.EntityID, vc.DisplayName
}

var errKubeconfigDisabled = fmt.Errorf("kubeconfig upload is disabled: %w", apperr.ErrNotFound)

func infos(f *kubeconfig.File) []Info {
	out := make([]Info, len(f.Contexts))
	for i, cx := range f.Contexts {
		out[i] = Info{ID: cx.ID, Name: cx.Name, Context: cx.Context, User: cx.User, Server: cx.Server}
	}
	return out
}

// storeKubeconfig keeps a parsed kubeconfig in the session, replacing the credentials and
// connections of an earlier upload with the same label.
func (c *Credentials) storeKubeconfig(ctx context.Context, sess session.Session, label, content string) error {
	if err := c.dropKubeconfigClusters(ctx, sess, label); err != nil {
		return err
	}
	sealed, err := c.d.Sealer.Seal([]byte(content), crypto.AAD(crypto.PurposeSessionCredential, sess.Hash))
	if err != nil {
		return fmt.Errorf("sealing kubeconfig: %w", err)
	}
	return c.d.Sessions.PutCredential(ctx, sess.Hash, store.CredentialRow{Kind: kindKubeconfig, Ref: label, Sealed: sealed}, c.d.Clock.Now())
}

// dropKubeconfigClusters deletes the cluster credentials of a kubeconfig and closes its connections.
func (c *Credentials) dropKubeconfigClusters(ctx context.Context, sess session.Session, label string) error {
	prefix := kubeconfigClusterPrefix + label + "/"
	rows, err := c.d.Sessions.ListCredentials(ctx, sess.Hash, kindClusterCred)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if strings.HasPrefix(r.Ref, prefix) {
			if err := c.d.Sessions.DeleteCredential(ctx, sess.Hash, kindClusterCred, r.Ref); err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
		}
	}
	return c.d.Registry.CloseMatching(sess.Hash, func(name string) bool { return strings.HasPrefix(name, prefix) })
}

// AddKubeconfig validates an uploaded kubeconfig, keeps it in the session and returns the sealed copy for the browser.
func (c *Credentials) AddKubeconfig(ctx context.Context, sess session.Session, label, content string) (sealed string, clusters []Info, err error) {
	if !c.d.Config.Kubeconfig.IsEnabled() {
		return "", nil, errKubeconfigDisabled
	}
	f, err := kubeconfig.Parse(label, []byte(content))
	if err != nil {
		return "", nil, err
	}
	if err := c.storeKubeconfig(ctx, sess, label, content); err != nil {
		return "", nil, err
	}
	sealed, err = c.sealForBrowser(sess.UserID, browserCopy{Kind: kindKubeconfig, Label: label, Content: content, SavedAt: c.d.Clock.Now().UTC()})
	if err != nil {
		return "", nil, err
	}
	return sealed, infos(f), nil
}

// RemoveKubeconfig deletes a kubeconfig, the credentials of its clusters and their open connections.
func (c *Credentials) RemoveKubeconfig(ctx context.Context, sess session.Session, label string) error {
	if err := c.d.Sessions.DeleteCredential(ctx, sess.Hash, kindKubeconfig, label); err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	return c.dropKubeconfigClusters(ctx, sess, label)
}

func (c *Credentials) restoreKubeconfig(ctx context.Context, sess session.Session, bc browserCopy) RestoreResult {
	res := RestoreResult{Kind: kindKubeconfig, Label: bc.Label}
	if !c.d.Config.Kubeconfig.IsEnabled() {
		res.Status, res.Message = StatusInvalid, "Kubeconfig upload is disabled on this server."
		return res
	}
	f, err := kubeconfig.Parse(bc.Label, []byte(bc.Content))
	if err != nil {
		res.Status, res.Message = StatusInvalid, err.Error()
		return res
	}
	if err := c.storeKubeconfig(ctx, sess, bc.Label, bc.Content); err != nil {
		c.d.Log.Warn("storing kubeconfig", "error", err.Error())
		res.Status = StatusInvalid
		return res
	}
	res.Status, res.Clusters = StatusOK, infos(f)
	return res
}
