package auth

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"time"

	"golang.org/x/oauth2"
)

// BeginReauthentication cannot create or link an identity. The flow is bound to
// the requesting active session, a browser cookie, nonce, PKCE and one-use state.
func (s *GoogleService) BeginReauthentication(ctx context.Context, browser, access, returnTo string) (GoogleFlow, error) {
	if s.provider == nil || browser == "" {
		return GoogleFlow{}, ErrInvalidOIDC
	}
	a, err := s.sessions.Authenticate(ctx, access)
	if err != nil {
		return GoogleFlow{}, err
	}
	sid, _, err := s.sessions.sessionDetails(access)
	if err != nil {
		return GoogleFlow{}, err
	}
	var linked bool
	if err = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM external_identities WHERE account_id=$1 AND provider='google')`, a.ID).Scan(&linked); err != nil {
		return GoogleFlow{}, err
	}
	if !linked {
		return GoogleFlow{}, ErrInvalidOIDC
	}
	state, err := randomCredential()
	if err != nil {
		return GoogleFlow{}, err
	}
	nonce, err := randomCredential()
	if err != nil {
		return GoogleFlow{}, err
	}
	verifier := oauth2.GenerateVerifier()
	now := s.now().UTC()
	expires := now.Add(ReauthenticationLifetime)
	destination, err := url.Parse(s.provider.AuthURL(state, nonce, verifier))
	if err != nil {
		return GoogleFlow{}, ErrInvalidOIDC
	}
	q := destination.Query()
	q.Set("max_age", "0")
	q.Set("prompt", "select_account")
	q.Set("claims", `{"id_token":{"auth_time":{"essential":true}}}`)
	destination.RawQuery = q.Encode()
	_, err = s.db.ExecContext(ctx, `INSERT INTO google_oidc_flows(state_digest,browser_digest,nonce,pkce_verifier,reauth_session_id,started_at,expires_at,return_to) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, tokenDigest(state), tokenDigest(browser), nonce, verifier, sid, now, expires, returnTo)
	if err != nil {
		return GoogleFlow{}, err
	}
	return GoogleFlow{URL: destination.String(), ExpiresAt: expires}, nil
}

// handled distinguishes this purpose from the existing login/link callback.
// Even a failed proof consumes its correctly browser-bound flow. No application
// tokens are created or rotated, and subjects are resolved only by existing links.
func (s *GoogleService) CompleteReauthentication(ctx context.Context, state, browser, code string) (handled bool, target string, err error) {
	if len(state) != 43 || browser == "" || code == "" || len(code) > 4096 {
		return false, "", ErrInvalidOIDC
	}
	if err = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM google_oidc_flows WHERE state_digest=$1 AND reauth_session_id IS NOT NULL)`, tokenDigest(state)).Scan(&handled); err != nil {
		return true, "", err
	}
	if !handled {
		return false, "", ErrInvalidOIDC
	}
	var sid, nonce, verifier string
	var started time.Time
	err = s.db.QueryRowContext(ctx, `DELETE FROM google_oidc_flows WHERE state_digest=$1 AND browser_digest=$2 AND reauth_session_id IS NOT NULL AND expires_at>$3 RETURNING reauth_session_id,nonce,pkce_verifier,started_at,return_to`, tokenDigest(state), tokenDigest(browser), s.now().UTC()).Scan(&sid, &nonce, &verifier, &started, &target)
	if errors.Is(err, sql.ErrNoRows) {
		return true, "", ErrInvalidOIDC
	}
	if err != nil {
		return true, "", err
	}
	identity, err := s.provider.Exchange(ctx, code, nonce, verifier)
	if err != nil {
		return true, target, ErrInvalidOIDC
	}
	now := s.now().UTC()
	// iat is issuance, not authentication. Never substitute it for auth_time.
	if !identity.EmailVerified || identity.Subject == "" || identity.AuthenticatedAt.IsZero() || identity.AuthenticatedAt.Before(started.Add(-30*time.Second)) || identity.AuthenticatedAt.After(now.Add(30*time.Second)) || !now.Before(identity.AuthenticatedAt.Add(ReauthenticationLifetime)) {
		return true, target, ErrInvalidOIDC
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return true, target, err
	}
	defer tx.Rollback()
	var account string
	err = tx.QueryRowContext(ctx, `SELECT a.id FROM accounts a JOIN auth_sessions s ON s.account_id=a.id WHERE s.id=$1 AND a.email_verified_at IS NOT NULL FOR SHARE OF a`, sid).Scan(&account)
	if errors.Is(err, sql.ErrNoRows) {
		return true, target, ErrInvalidSession
	}
	if err != nil {
		return true, target, err
	}
	var subject string
	err = tx.QueryRowContext(ctx, `SELECT subject FROM external_identities WHERE account_id=$1 AND provider='google' FOR SHARE`, account).Scan(&subject)
	if errors.Is(err, sql.ErrNoRows) {
		return true, target, ErrInvalidOIDC
	}
	if err != nil {
		return true, target, err
	}
	if subject != identity.Subject {
		return true, target, ErrInvalidOIDC
	}
	if err = s.sessions.recordReauthentication(ctx, tx, account, sid, "google", minTime(now, identity.AuthenticatedAt)); err != nil {
		return true, target, err
	}
	if err = tx.Commit(); err != nil {
		return true, target, err
	}
	return true, target, nil
}
