package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
)

func NewVerificationService(db *sql.DB, config VerificationConfig) (*VerificationService, error) {
	if len(config.Key) != 32 {
		return nil, ErrVerificationUnavailable
	}
	digestKey, err := hkdf.Key(sha256.New, config.Key, nil, "city4christ/verification-digest/v1", 32)
	if err != nil {
		return nil, err
	}
	emailKey, err := hkdf.Key(sha256.New, config.Key, nil, "city4christ/verification-email/v1", 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(emailKey)
	if err != nil {
		return nil, err
	}
	emailCipher, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return nil, err
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &VerificationService{db: db, digestKey: digestKey, emailCipher: emailCipher, now: now}, nil
}

func (s *VerificationService) digest(accountID, challengeID, code string) []byte {
	mac := hmac.New(sha256.New, s.digestKey)
	_, _ = mac.Write([]byte("email-verification:v1:" + accountID + ":" + challengeID + ":" + code))
	return mac.Sum(nil)
}

// OpenEmail is for trusted delivery/inbox adapters only. It authenticates the
// ciphertext's account and challenge binding. HTTP responses never expose it.
func (s *VerificationService) OpenEmail(accountID, challengeID string, sealed []byte) (VerificationEmail, error) {
	plaintext, err := s.emailCipher.Open(nil, nil, sealed, emailBinding(accountID, challengeID))
	if err != nil {
		return VerificationEmail{}, err
	}
	var email VerificationEmail
	if err := json.Unmarshal(plaintext, &email); err != nil {
		return VerificationEmail{}, err
	}
	return email, nil
}

func emailBinding(accountID, challengeID string) []byte {
	return []byte("email-verification:v1:" + accountID + ":" + challengeID)
}

func (s *VerificationService) matches(accountID, challengeID, code string, stored []byte) bool {
	return hmac.Equal(stored, s.digest(accountID, challengeID, code))
}

type issuedVerification struct {
	id        string
	digest    []byte
	expiresAt time.Time
	sealed    []byte
}

func (s *VerificationService) prepareEmail(account Account, previous *verificationChallenge, now time.Time) (issuedVerification, error) {

	var code string
	for {
		n, err := rand.Int(rand.Reader, big.NewInt(100000000))
		if err != nil {
			return issuedVerification{}, err
		}
		code = fmt.Sprintf("%08d", n.Int64())
		// Even an accidental random collision must not reactivate the previous code.
		if previous == nil || !hmac.Equal(previous.digest, s.digest(account.ID, previous.id, code)) {
			break
		}
	}
	id := uuid.NewString()
	expiresAt := now.Add(VerificationLifetime)
	payload, err := json.Marshal(VerificationEmail{Email: account.Email, Code: code, ExpiresAt: expiresAt})
	if err != nil {
		return issuedVerification{}, err
	}
	sealed := s.emailCipher.Seal(nil, nil, payload, emailBinding(account.ID, id))
	return issuedVerification{id: id, digest: s.digest(account.ID, id, code), expiresAt: expiresAt, sealed: sealed}, nil
}
