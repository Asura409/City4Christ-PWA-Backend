// Package visitors keeps consented visitor contact requests separate from accounts.
package visitors

import (
	"errors"
	"github.com/google/uuid"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const PolicyVersion = "visitor-welcome-v1"
const RetentionDays = 90
const ConsentText = "I agree that City4Christ may store these details for up to 90 days and contact me about my visit. This is not consent to marketing or a mailing list."

var (
	ErrInvalid     = errors.New("invalid visitor fields")
	ErrForbidden   = errors.New("visitor follow-up forbidden")
	ErrNotFound    = errors.New("visitor record not found")
	ErrConflict    = errors.New("visitor request or version conflict")
	ErrUnavailable = errors.New("visitor service unavailable")
)

type ValidationError struct{ Fields map[string]string }

func (e *ValidationError) Error() string { return ErrInvalid.Error() }
func (e *ValidationError) Unwrap() error { return ErrInvalid }

type Submission struct {
	SubmissionID  string `json:"submissionId"`
	FullName      string `json:"fullName"`
	Email         string `json:"email"`
	Phone         string `json:"phone"`
	Consent       bool   `json:"consent"`
	PolicyVersion string `json:"policyVersion"`
}
type contact struct {
	FullName string `json:"fullName"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
}
type Summary struct {
	ID        string    `json:"id"`
	FullName  string    `json:"fullName"`
	Status    string    `json:"status"`
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}
type Detail struct {
	Summary
	Email           string `json:"email"`
	Phone           string `json:"phone"`
	ContactVerified bool   `json:"contactVerified"`
	PolicyVersion   string `json:"policyVersion"`
	ConsentText     string `json:"consentText"`
}
type Options struct {
	Limit          int
	Cursor, Status string
}
type Page struct {
	Items      []Summary `json:"items"`
	NextCursor string    `json:"nextCursor,omitempty"`
}
type Update struct {
	Status  string `json:"status"`
	Version int    `json:"version"`
}

func validID(s string) bool {
	id, e := uuid.Parse(s)
	return e == nil && id != uuid.Nil && id.String() == s
}

var phoneFormat = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)

func validText(v string, max int) bool {
	if !utf8.ValidString(v) || utf8.RuneCountInString(v) > max {
		return false
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func normalize(in Submission) (Submission, error) {
	in.FullName = strings.TrimSpace(in.FullName)
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Phone = strings.TrimSpace(in.Phone)
	fields := map[string]string{}
	if !validID(in.SubmissionID) {
		fields["submissionId"] = "Start a new welcome form."
	}
	if in.FullName == "" || !validText(in.FullName, 200) {
		fields["fullName"] = "Enter a name of up to 200 characters."
	}
	if in.Email != "" {
		a, e := mail.ParseAddress(in.Email)
		if len(in.Email) > 254 || !validText(in.Email, 254) || e != nil || a.Address != in.Email {
			fields["email"] = "Enter a valid email address."
		}
	}
	if in.Phone != "" && !phoneFormat.MatchString(in.Phone) {
		fields["phone"] = "Use international format, starting with + and the country code."
	}
	if in.Email == "" && in.Phone == "" {
		fields["email"] = "Provide an email address or phone number."
		fields["phone"] = "Provide a phone number or email address."
	}
	if !in.Consent {
		fields["consent"] = "Consent is required to store your details and follow up."
	}
	if in.PolicyVersion != PolicyVersion {
		fields["policyVersion"] = "Reload the current privacy notice before submitting."
	}
	if len(fields) > 0 {
		return in, &ValidationError{fields}
	}
	return in, nil
}
