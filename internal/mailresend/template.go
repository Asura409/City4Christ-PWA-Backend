package mailresend

import (
	"bytes"
	_ "embed"
	"html/template"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
)

//go:embed verification.html
var verificationHTML string

var verificationTemplate = template.Must(template.New("verification").Parse(verificationHTML))

func renderVerificationHTML(message auth.VerificationEmail) (string, error) {
	var body bytes.Buffer
	expiry := message.ExpiresAt.UTC()
	err := verificationTemplate.Execute(&body, struct{ Code, ExpiryISO, ExpiryLabel string }{message.Code, expiry.Format("2006-01-02T15:04:05Z07:00"), expiry.Format("2 Jan 2006 at 15:04 UTC")})
	return body.String(), err
}
