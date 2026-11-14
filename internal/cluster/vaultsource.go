package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"k8s.io/client-go/rest"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/store"
	"github.com/bdnix/kubelook/internal/vault"
)

// vaultClusterCred is the plaintext of the sealed cluster_cred session credential of a Vault cluster.
type vaultClusterCred struct {
	Host           string    `json:"host"`
	CACert         string    `json:"caCert"`
	Token          string    `json:"token"`
	ServiceAccount string    `json:"serviceAccount"`
	LeaseID        string    `json:"leaseId"`
	Role           string    `json:"role"`
	MintedAt       time.Time `json:"mintedAt"`
	ExpiresAt      time.Time `json:"expiresAt"`
}

// vaultSource mints short-lived service account tokens from Vault's Kubernetes secrets engine.
type vaultSource struct{ o *Opener }

// mapVaultError turns a Vault failure into the error the user sees.
func mapVaultError(err error) error {
	var re *vault.ResponseError
	switch {
	case errors.Is(err, vault.ErrUnreachable):
		return errVaultDown
	case errors.Is(err, vault.ErrPermissionDenied):
		return errVaultTokenGone
	case errors.As(err, &re) && (re.Status == http.StatusBadRequest || re.Status == http.StatusInternalServerError):
		return &apperr.APIError{Status: http.StatusUnprocessableEntity, Code: "unprocessable", Message: strings.Join(re.Messages, "; ")}
	}
	return fmt.Errorf("minting cluster credentials: %w", err)
}

func (s *vaultSource) chooseRole(ctx context.Context, sess session.Session, ref Ref, role string) (string, error) {
	if role != "" {
		if !slices.Contains(ref.Roles, role) {
			return "", fmt.Errorf("you are not allowed to use the role %q on this cluster: %w", role, apperr.ErrForbidden)
		}
		return role, nil
	}
	if remembered, ok := s.rememberedRole(ctx, sess, ref.ID); ok && slices.Contains(ref.Roles, remembered) {
		return remembered, nil
	}
	if len(ref.Roles) == 0 {
		return "", fmt.Errorf("no role is available on this cluster: %w", apperr.ErrForbidden)
	}
	return ref.Roles[0], nil
}

func (s *vaultSource) rememberedRole(ctx context.Context, sess session.Session, id string) (string, bool) {
	row, err := s.o.d.Sessions.GetCredential(ctx, sess.Hash, kindClusterRole, id)
	if err != nil {
		return "", false
	}
	pt, err := s.o.d.Credentials.openSessionCredential(ctx, sess, row.Sealed)
	if err != nil {
		return "", false
	}
	return string(pt), true
}

func (s *vaultSource) open(ctx context.Context, sess session.Session, ref Ref, role string) (*entry, error) {
	o := s.o
	role, err := s.chooseRole(ctx, sess, ref, role)
	if err != nil {
		return nil, err
	}
	vc, err := o.d.Credentials.getVaultCred(ctx, sess)
	if err != nil {
		return nil, err
	}
	cc, err := o.d.Vault.ClusterConfig(ctx, vc.Token, ref.Mount)
	if err != nil {
		return nil, mapVaultError(err)
	}
	if cc.Host == "" || cc.CACert == "" {
		return nil, errNoAPIConfig
	}
	cred, err := s.mint(ctx, ref, role, vc.Token, cc.Host, cc.CACert)
	if err != nil {
		return nil, err
	}
	if err := s.persist(ctx, sess, ref.ID, cred); err != nil {
		return nil, err
	}
	sealedRole, err := o.sealSession(sess, []byte(role))
	if err != nil {
		return nil, err
	}
	if err := o.d.Sessions.PutCredential(ctx, sess.Hash, store.CredentialRow{Kind: kindClusterRole, Ref: ref.ID, Sealed: sealedRole}, o.d.Clock.Now()); err != nil {
		return nil, err
	}
	return &entry{vcred: cred, role: role, server: cc.Host, expires: cred.ExpiresAt}, nil
}

// mint asks Vault for a new service account token and audits it.
func (s *vaultSource) mint(ctx context.Context, ref Ref, role, vaultToken, host, ca string) (vaultClusterCred, error) {
	o := s.o
	cfg := o.d.Config.Vault
	c, err := o.d.Vault.Creds(ctx, vaultToken, ref.Mount, role, vault.CredsRequest{
		Namespace: cfg.CredsNamespace, ClusterRoleBinding: cfg.ClusterRoleBinding(), TTL: cfg.TokenTTL.D(),
	})
	if err != nil {
		return vaultClusterCred{}, mapVaultError(err)
	}
	now := o.d.Clock.Now()
	lease := c.LeaseDuration
	if lease <= 0 {
		lease = cfg.TokenTTL.D()
	}
	o.auditIssued(ctx, ref.ID, ref.Mount+"/creds/"+role, role, c.LeaseID)
	return vaultClusterCred{
		Host: host, CACert: ca, Token: c.Token, ServiceAccount: c.ServiceAccount, LeaseID: c.LeaseID,
		Role: role, MintedAt: now.UTC(), ExpiresAt: now.Add(lease).UTC(),
	}, nil
}

func (s *vaultSource) persist(ctx context.Context, sess session.Session, id string, cred vaultClusterCred) error {
	b, err := json.Marshal(cred)
	if err != nil {
		return fmt.Errorf("encoding cluster credential: %w", err)
	}
	sealed, err := s.o.sealSession(sess, b)
	if err != nil {
		return err
	}
	return s.o.d.Sessions.PutCredential(ctx, sess.Hash, store.CredentialRow{Kind: kindClusterCred, Ref: id, Sealed: sealed, ExpiresAt: cred.ExpiresAt}, s.o.d.Clock.Now())
}

func (s *vaultSource) current(e *entry) vaultClusterCred {
	s.o.mu.Lock()
	defer s.o.mu.Unlock()
	return e.vcred
}

func (s *vaultSource) due(e *entry) bool {
	return s.current(e).ExpiresAt.Sub(s.o.d.Clock.Now()) <= s.o.d.Config.Vault.RemintBefore.D()
}

func (s *vaultSource) token(ctx context.Context, e *entry) (string, error) {
	o := s.o
	remint := o.d.Config.Vault.RemintBefore.D()
	cred := s.current(e)
	if cred.ExpiresAt.Sub(o.d.Clock.Now()) > remint {
		return cred.Token, nil
	}
	err := o.flight.do(entryKey(e.sess.Hash, e.ref.ID), func() error { return s.remint(ctx, e) })
	if err != nil {
		if errors.Is(err, errVaultDown) && o.d.Clock.Now().Before(cred.ExpiresAt) {
			return cred.Token, nil // Vault is down but the credential still works
		}
		return "", err
	}
	return s.current(e).Token, nil
}

func (s *vaultSource) remint(ctx context.Context, e *entry) error {
	o := s.o
	cur := s.current(e)
	if cur.ExpiresAt.Sub(o.d.Clock.Now()) > o.d.Config.Vault.RemintBefore.D() {
		return nil // another caller got there first
	}
	vc, err := o.d.Credentials.getVaultCred(ctx, e.sess)
	if err != nil {
		return err
	}
	cred, err := s.mint(ctx, e.ref, cur.Role, vc.Token, cur.Host, cur.CACert)
	if errors.Is(err, errVaultTokenGone) {
		o.setDisconnected(e)
	}
	if err != nil {
		return err
	}
	if err := s.persist(ctx, e.sess, e.ref.ID, cred); err != nil {
		return err
	}
	o.mu.Lock()
	e.vcred, e.expires = cred, cred.ExpiresAt
	o.mu.Unlock()
	return nil
}

func (s *vaultSource) restConfig(e *entry) (*rest.Config, error) {
	c := s.current(e)
	return &rest.Config{Host: c.Host, TLSClientConfig: rest.TLSClientConfig{CAData: []byte(c.CACert)}}, nil
}
