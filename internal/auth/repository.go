package auth

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrAccountNotFound = errors.New("account not found")
	ErrEmailTaken      = errors.New("email already registered")
)

type Account struct {
	ID              string
	Email           string
	FirstName       string
	LastName        string
	PasswordHash    *string
	EmailVerifiedAt *time.Time
	CreatedAt       time.Time
}

type NewAccount struct {
	Email        string
	FirstName    string
	LastName     string
	PasswordHash *string
}

type AccountRepository struct{ db *sql.DB }

func NewAccountRepository(db *sql.DB) *AccountRepository { return &AccountRepository{db: db} }

const accountColumns = "id, email, first_name, last_name, password_hash, email_verified_at, created_at"

func scanAccount(row *sql.Row) (Account, error) {
	var a Account
	err := row.Scan(&a.ID, &a.Email, &a.FirstName, &a.LastName, &a.PasswordHash, &a.EmailVerifiedAt, &a.CreatedAt)
	return a, err
}

type accountWriter interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (r *AccountRepository) Create(ctx context.Context, input NewAccount) (Account, error) {
	return createAccount(ctx, r.db, input)
}

func createAccount(ctx context.Context, db accountWriter, input NewAccount) (Account, error) {
	a, err := scanAccount(db.QueryRowContext(ctx, "INSERT INTO accounts(email,first_name,last_name,password_hash) VALUES ($1,$2,$3,$4) RETURNING "+accountColumns, normalizeEmail(input.Email), input.FirstName, input.LastName, input.PasswordHash))
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) && pgerr.Code == "23505" && pgerr.ConstraintName == "accounts_email_key" {
		return Account{}, ErrEmailTaken
	}
	return a, err
}

func (r *AccountRepository) ByEmail(ctx context.Context, email string) (Account, error) {
	a, err := scanAccount(r.db.QueryRowContext(ctx, "SELECT "+accountColumns+" FROM accounts WHERE email=$1", normalizeEmail(email)))
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrAccountNotFound
	}
	return a, err
}

func normalizeEmail(email string) string { return strings.ToLower(strings.TrimSpace(email)) }
