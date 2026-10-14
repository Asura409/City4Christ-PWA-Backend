package auth

import (
	"context"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrEmailUnverified    = errors.New("email unverified")
	ErrInvalidSession     = errors.New("invalid session")
	ErrRefreshReplay      = errors.New("refresh replay detected")
)

const AccessLifetime = 10 * time.Minute
const RefreshLifetime = 30 * 24 * time.Hour

type SessionConfig struct {
	Key []byte
	Now func() time.Time
}
type SessionTokens struct {
	AccessToken, RefreshToken         string
	AccessExpiresAt, RefreshExpiresAt time.Time
}
type SessionService struct {
	db  *sql.DB
	key []byte
	now func() time.Time
}
type sessionClaims struct {
	SessionID string `json:"sid"`
	jwt.RegisteredClaims
}

var dummyPassword struct {
	sync.Once
	hash string
	err  error
}

func NewSessionService(db *sql.DB, c SessionConfig) (*SessionService, error) {
	if len(c.Key) != 32 {
		return nil, ErrInvalidSession
	}
	key, err := hkdf.Key(sha256.New, c.Key, nil, "city4christ/access-jwt/v1", 32)
	if err != nil {
		return nil, err
	}
	now := c.Now
	if now == nil {
		now = time.Now
	}
	return &SessionService{db: db, key: key, now: now}, nil
}

func (s *SessionService) Login(ctx context.Context, email, password string) (SessionTokens, error) {
	dummyPassword.Do(func() { dummyPassword.hash, dummyPassword.err = NewPasswordHasher().Hash("non-user dummy credential") })
	if dummyPassword.err != nil {
		return SessionTokens{}, dummyPassword.err
	}

	if len(password) > 1024 || len(email) > 254 {
		return SessionTokens{}, ErrInvalidCredentials
	}
	a, err := NewAccountRepository(s.db).ByEmail(ctx, email)
	if err != nil && !errors.Is(err, ErrAccountNotFound) {
		return SessionTokens{}, err
	}
	hash := dummyPassword.hash
	if err == nil && a.PasswordHash != nil {
		hash = *a.PasswordHash
	}
	valid := NewPasswordHasher().Verify(password, hash)
	if err != nil || a.PasswordHash == nil || !valid {
		return SessionTokens{}, ErrInvalidCredentials
	}
	return s.Start(ctx, a.ID)
}

func randomCredential() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func tokenDigest(token string) []byte { sum := sha256.Sum256([]byte(token)); return sum[:] }

func (s *SessionService) Start(ctx context.Context, accountID string) (SessionTokens, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SessionTokens{}, err
	}
	defer tx.Rollback()
	var verified *time.Time
	if err := tx.QueryRowContext(ctx, `SELECT email_verified_at FROM accounts WHERE id=$1 FOR UPDATE`, accountID).Scan(&verified); err != nil {
		return SessionTokens{}, err
	}
	if verified == nil {
		return SessionTokens{}, ErrEmailUnverified
	}
	now := s.now().UTC()
	expires := now.Add(RefreshLifetime)
	sid := uuid.NewString()
	if _, err := tx.ExecContext(ctx, `INSERT INTO auth_sessions(id,account_id,expires_at,created_at) VALUES($1,$2,$3,$4)`, sid, accountID, expires, now); err != nil {
		return SessionTokens{}, err
	}
	tokens, err := s.issue(ctx, tx, accountID, sid, now, expires)
	if err != nil {
		return SessionTokens{}, err
	}
	if err := tx.Commit(); err != nil {
		return SessionTokens{}, err
	}
	return tokens, nil
}

func (s *SessionService) issue(ctx context.Context, tx *sql.Tx, accountID, sid string, now, expires time.Time) (SessionTokens, error) {
	raw, err := randomCredential()
	if err != nil {
		return SessionTokens{}, err
	}
	accessExpiry := minTime(now.Add(AccessLifetime), expires)
	claims := sessionClaims{SessionID: sid, RegisteredClaims: jwt.RegisteredClaims{Issuer: "city4christ", Subject: accountID, Audience: jwt.ClaimStrings{"city4christ-api"}, ExpiresAt: jwt.NewNumericDate(accessExpiry), IssuedAt: jwt.NewNumericDate(now), NotBefore: jwt.NewNumericDate(now), ID: uuid.NewString()}}
	access, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.key)
	if err != nil {
		return SessionTokens{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO refresh_tokens(token_digest,session_id,created_at) VALUES($1,$2,$3)`, tokenDigest(raw), sid, now); err != nil {
		return SessionTokens{}, err
	}
	return SessionTokens{AccessToken: access, RefreshToken: raw, AccessExpiresAt: accessExpiry, RefreshExpiresAt: expires}, nil
}
func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func (s *SessionService) Refresh(ctx context.Context, raw string) (SessionTokens, error) {
	if len(raw) != 43 {
		return SessionTokens{}, ErrInvalidSession
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SessionTokens{}, err
	}
	defer tx.Rollback()
	var sid, accountID string
	var expires time.Time
	var revoked *time.Time
	err = tx.QueryRowContext(ctx, `SELECT s.id,s.account_id,s.expires_at,s.revoked_at FROM auth_sessions s JOIN refresh_tokens t ON t.session_id=s.id WHERE t.token_digest=$1 FOR UPDATE OF s`, tokenDigest(raw)).Scan(&sid, &accountID, &expires, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionTokens{}, ErrInvalidSession
	}
	if err != nil {
		return SessionTokens{}, err
	}
	now := s.now().UTC()
	if revoked != nil || !now.Before(expires) {
		return SessionTokens{}, ErrInvalidSession
	}
	var consumed *time.Time
	if err := tx.QueryRowContext(ctx, `SELECT consumed_at FROM refresh_tokens WHERE token_digest=$1`, tokenDigest(raw)).Scan(&consumed); err != nil {
		return SessionTokens{}, err
	}
	if consumed != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE auth_sessions SET revoked_at=$2 WHERE id=$1`, sid, now); err != nil {
			return SessionTokens{}, err
		}
		if err := tx.Commit(); err != nil {
			return SessionTokens{}, err
		}
		return SessionTokens{}, ErrRefreshReplay
	}
	if _, err := tx.ExecContext(ctx, `UPDATE refresh_tokens SET consumed_at=$2 WHERE token_digest=$1`, tokenDigest(raw), now); err != nil {
		return SessionTokens{}, err
	}
	tokens, err := s.issue(ctx, tx, accountID, sid, now, expires)
	if err != nil {
		return SessionTokens{}, err
	}
	if err := tx.Commit(); err != nil {
		return SessionTokens{}, err
	}
	return tokens, nil
}

func (s *SessionService) Authenticate(ctx context.Context, raw string) (Account, error) {
	claims, err := s.parseAccess(raw)
	if err != nil {
		return Account{}, err
	}
	a, err := scanAccount(s.db.QueryRowContext(ctx, `SELECT `+prefixedAccountColumns+` FROM accounts a JOIN auth_sessions s ON s.account_id=a.id WHERE s.id=$1 AND a.id=$2 AND s.revoked_at IS NULL AND s.expires_at>$3 AND a.email_verified_at IS NOT NULL`, claims.SessionID, claims.Subject, s.now().UTC()))
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrInvalidSession
	}
	return a, err
}

var prefixedAccountColumns = "a." + strings.ReplaceAll(accountColumns, ", ", ", a.")

func (s *SessionService) Logout(ctx context.Context, raw string) error {
	if len(raw) != 43 {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `UPDATE auth_sessions SET revoked_at=COALESCE(revoked_at,$2) WHERE id=(SELECT session_id FROM refresh_tokens WHERE token_digest=$1)`, tokenDigest(raw), s.now().UTC())
	return err
}

func (s *SessionService) parseAccess(raw string) (*sessionClaims, error) {
	if len(raw) > 4096 || raw == "" {
		return nil, ErrInvalidSession
	}
	claims := new(sessionClaims)
	_, err := jwt.ParseWithClaims(raw, claims, func(*jwt.Token) (any, error) { return s.key, nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer("city4christ"), jwt.WithAudience("city4christ-api"), jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithTimeFunc(s.now))
	if err != nil || claims.SessionID == "" || claims.Subject == "" || claims.IssuedAt == nil || claims.ID == "" {
		return nil, ErrInvalidSession
	}
	if _, err := uuid.Parse(claims.SessionID); err != nil {
		return nil, ErrInvalidSession
	}
	if _, err := uuid.Parse(claims.Subject); err != nil {
		return nil, ErrInvalidSession
	}
	return claims, nil
}
func (s *SessionService) sessionDetails(raw string) (string, time.Time, error) {
	claims, err := s.parseAccess(raw)
	if err != nil {
		return "", time.Time{}, err
	}
	return claims.SessionID, claims.IssuedAt.Time, nil
}
