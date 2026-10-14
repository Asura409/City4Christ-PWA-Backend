package auth

import (
	"context"
	"database/sql"
	"errors"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/oauth2"
)

var (
	ErrInvalidOIDC        = errors.New("invalid OpenID Connect flow")
	ErrLinkRequired       = errors.New("existing account requires explicit linking")
	ErrRegistrationClosed = errors.New("registration closed")
	ErrIdentityConflict   = errors.New("identity already linked")
)

type GoogleIdentity struct {
	Subject, Email, FirstName, LastName string
	EmailVerified                       bool
}
type GoogleProvider interface {
	AuthURL(state, nonce, verifier string) string
	Exchange(ctx context.Context, code, nonce, verifier string) (GoogleIdentity, error)
}
type GoogleFlow struct {
	URL       string
	ExpiresAt time.Time
}
type GoogleService struct {
	db       *sql.DB
	sessions *SessionService
	provider GoogleProvider
	signup   bool
	now      func() time.Time
}

func NewGoogleService(db *sql.DB, sessions *SessionService, provider GoogleProvider, signup bool, now func() time.Time) *GoogleService {
	if now == nil {
		now = time.Now
	}
	return &GoogleService{db: db, sessions: sessions, provider: provider, signup: signup, now: now}
}
func (s *GoogleService) Begin(ctx context.Context, browser, access string) (GoogleFlow, error) {
	if browser == "" || s.provider == nil {
		return GoogleFlow{}, ErrInvalidOIDC
	}
	var accountID, sessionID *string
	now := s.now().UTC()
	expires := now.Add(10 * time.Minute)
	if access != "" {
		a, err := s.sessions.Authenticate(ctx, access)
		if err != nil {
			return GoogleFlow{}, err
		}
		sid, _, err := s.sessions.sessionDetails(access)
		if err != nil {
			return GoogleFlow{}, err
		}
		// Refreshing an access token does not constitute password/provider reauthentication.
		var authenticatedAt time.Time
		if err := s.db.QueryRowContext(ctx, `SELECT created_at FROM auth_sessions WHERE id=$1`, sid).Scan(&authenticatedAt); err != nil {
			return GoogleFlow{}, err
		}
		if now.Sub(authenticatedAt) >= 5*time.Minute {
			return GoogleFlow{}, ErrInvalidSession
		}
		accountID = &a.ID
		sessionID = &sid
		expires = minTime(expires, authenticatedAt.Add(5*time.Minute))
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
	_, err = s.db.ExecContext(ctx, `INSERT INTO google_oidc_flows(state_digest,browser_digest,nonce,pkce_verifier,link_account_id,link_session_id,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, tokenDigest(state), tokenDigest(browser), nonce, verifier, accountID, sessionID, expires)
	if err != nil {
		return GoogleFlow{}, err
	}
	return GoogleFlow{URL: s.provider.AuthURL(state, nonce, verifier), ExpiresAt: expires}, nil
}
func (s *GoogleService) Complete(ctx context.Context, state, browser, code string) (SessionTokens, error) {
	if len(state) != 43 || browser == "" || code == "" || len(code) > 4096 {
		return SessionTokens{}, ErrInvalidOIDC
	}
	var nonce, verifier string
	var accountID, sessionID *string
	// Atomic deletion ensures exactly one callback can exchange the authorization code.
	err := s.db.QueryRowContext(ctx, `DELETE FROM google_oidc_flows WHERE state_digest=$1 AND browser_digest=$2 AND expires_at>$3 RETURNING nonce,pkce_verifier,link_account_id,link_session_id`, tokenDigest(state), tokenDigest(browser), s.now().UTC()).Scan(&nonce, &verifier, &accountID, &sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionTokens{}, ErrInvalidOIDC
	}
	if err != nil {
		return SessionTokens{}, err
	}
	identity, err := s.provider.Exchange(ctx, code, nonce, verifier)
	if err != nil {
		return SessionTokens{}, ErrInvalidOIDC
	}
	identity.Email = normalizeEmail(identity.Email)
	address, err := mail.ParseAddress(identity.Email)
	if !identity.EmailVerified || identity.Subject == "" || len(identity.Subject) > 255 || err != nil || address.Address != identity.Email || len(identity.Email) > 254 {
		return SessionTokens{}, ErrInvalidOIDC
	}
	id, err := s.resolve(ctx, identity, accountID, sessionID)
	if err != nil {
		return SessionTokens{}, err
	}
	return s.sessions.Start(ctx, id)
}
func googleName(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	r := []rune(value)
	if len(r) > 100 {
		r = r[:100]
	}
	return string(r)
}
func (s *GoogleService) resolve(ctx context.Context, identity GoogleIdentity, linkAccount, linkSession *string) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	now := s.now().UTC()
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT account_id FROM external_identities WHERE provider='google' AND subject=$1`, identity.Subject).Scan(&existing)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if linkAccount != nil {
		var email string
		// Lock the authorizing session so logout/replay cannot race the link decision.
		err := tx.QueryRowContext(ctx, `SELECT a.email FROM auth_sessions s JOIN accounts a ON a.id=s.account_id WHERE s.id=$1 AND a.id=$2 AND s.revoked_at IS NULL AND s.expires_at>$3 AND s.created_at>$4 AND a.email_verified_at IS NOT NULL FOR UPDATE OF s`, *linkSession, *linkAccount, now, now.Add(-5*time.Minute)).Scan(&email)
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrInvalidSession
		}
		if err != nil {
			return "", err
		}
		if email != identity.Email {
			return "", ErrInvalidOIDC
		}
		if existing != "" && existing != *linkAccount {
			return "", ErrIdentityConflict
		}
		if existing == *linkAccount {
			if err := tx.Commit(); err != nil {
				return "", err
			}
			return existing, nil
		}
		existing = *linkAccount
	} else if existing != "" {
		if err := tx.Commit(); err != nil {
			return "", err
		}
		return existing, nil
	} else {
		var localID string
		err := tx.QueryRowContext(ctx, `SELECT id FROM accounts WHERE email=$1`, identity.Email).Scan(&localID)
		if err == nil {
			return "", ErrLinkRequired
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
		if !s.signup {
			return "", ErrRegistrationClosed
		}
		if !utf8.ValidString(identity.FirstName) || !utf8.ValidString(identity.LastName) {
			return "", ErrInvalidOIDC
		}
		a, err := createAccount(ctx, tx, NewAccount{Email: identity.Email, FirstName: googleName(identity.FirstName, "Google"), LastName: googleName(identity.LastName, "Member")})
		if errors.Is(err, ErrEmailTaken) {
			return "", ErrLinkRequired
		}
		if err != nil {
			return "", err
		}
		existing = a.ID
		if _, err := tx.ExecContext(ctx, `UPDATE accounts SET email_verified_at=$2 WHERE id=$1`, existing, now); err != nil {
			return "", err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO external_identities(provider,subject,account_id) VALUES('google',$1,$2)`, identity.Subject, existing); err != nil {
		var pgerr *pgconn.PgError
		if errors.As(err, &pgerr) && pgerr.Code == "23505" {
			return "", ErrIdentityConflict
		}
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return existing, nil
}
