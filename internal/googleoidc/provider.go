// Package googleoidc delegates OIDC signature and claims validation to coreos/go-oidc.
package googleoidc

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type Provider struct {
	oauth    oauth2.Config
	verifier *oidc.IDTokenVerifier
	client   *http.Client
}

func New(ctx context.Context, clientID, secret, redirect string) (*Provider, error) {
	u, err := url.Parse(redirect)
	if strings.TrimSpace(clientID) == "" || strings.TrimSpace(secret) == "" || err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("valid Google credentials and HTTPS callback are required")
	}
	return newProvider(ctx, "https://accounts.google.com", clientID, secret, redirect)
}
func newProvider(ctx context.Context, issuer, clientID, secret, redirect string) (*Provider, error) {
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	ctx = oidc.ClientContext(ctx, client)
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, errors.New("Google provider discovery failed")
	}
	return &Provider{oauth: oauth2.Config{ClientID: clientID, ClientSecret: secret, RedirectURL: redirect, Endpoint: provider.Endpoint(), Scopes: []string{oidc.ScopeOpenID, "email", "profile"}}, verifier: provider.Verifier(&oidc.Config{ClientID: clientID, SupportedSigningAlgs: []string{"RS256"}}), client: client}, nil
}
func (p *Provider) AuthURL(state, nonce, verifier string) string {
	return p.oauth.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier))
}
func (p *Provider) Exchange(ctx context.Context, code, nonce, verifier string) (auth.GoogleIdentity, error) {
	ctx = oidc.ClientContext(ctx, p.client)
	token, err := p.oauth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return auth.GoogleIdentity{}, auth.ErrInvalidOIDC
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok || raw == "" {
		return auth.GoogleIdentity{}, auth.ErrInvalidOIDC
	}
	id, err := p.verifier.Verify(ctx, raw)
	if err != nil || nonce == "" || id.Nonce != nonce {
		return auth.GoogleIdentity{}, auth.ErrInvalidOIDC
	}
	var claims struct {
		Email           string `json:"email"`
		EmailVerified   bool   `json:"email_verified"`
		GivenName       string `json:"given_name"`
		FamilyName      string `json:"family_name"`
		AuthorizedParty string `json:"azp"`
	}
	if err := id.Claims(&claims); err != nil {
		return auth.GoogleIdentity{}, auth.ErrInvalidOIDC
	}
	if (len(id.Audience) > 1 || claims.AuthorizedParty != "") && claims.AuthorizedParty != p.oauth.ClientID {
		return auth.GoogleIdentity{}, auth.ErrInvalidOIDC
	}
	return auth.GoogleIdentity{Subject: id.Subject, Email: claims.Email, EmailVerified: claims.EmailVerified, FirstName: claims.GivenName, LastName: claims.FamilyName}, nil
}
