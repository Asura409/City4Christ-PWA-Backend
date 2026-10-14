package googleoidc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"golang.org/x/oauth2"
)

func TestOIDCAdapterValidatesSignedTokensNonceAndPKCE(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithHeader("kid", "test"))
	if err != nil {
		t.Fatal(err)
	}
	verifier := oauth2.GenerateVerifier()
	claims := map[string]any{"sub": "subject", "email": "google@example.invalid", "email_verified": true, "given_name": "Google", "family_name": "Tester", "aud": "client", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "nonce": "nonce"}
	var base string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{"issuer": base, "authorization_endpoint": base + "/authorize", "token_endpoint": base + "/token", "jwks_uri": base + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}})
		case "/keys":
			_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "test", Algorithm: "RS256", Use: "sig"}}})
		case "/token":
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			if r.Form.Get("code_verifier") != verifier || r.Form.Get("redirect_uri") != "https://api.example.invalid/callback" {
				t.Error("exchange omitted PKCE/redirect")
			}
			raw, _ := json.Marshal(claims)
			signed, err := signer.Sign(raw)
			if err != nil {
				t.Error(err)
			}
			token, _ := signed.CompactSerialize()
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "provider-access", "token_type": "Bearer", "id_token": token})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	base = server.URL
	claims["iss"] = base
	provider, err := newProvider(context.Background(), base, "client", "secret", "https://api.example.invalid/callback")
	if err != nil {
		t.Fatal(err)
	}
	authURL, err := url.Parse(provider.AuthURL("state", "nonce", verifier))
	if err != nil {
		t.Fatal(err)
	}
	if authURL.Query().Get("state") != "state" || authURL.Query().Get("nonce") != "nonce" || authURL.Query().Get("code_challenge_method") != "S256" || authURL.Query().Get("code_challenge") == verifier {
		t.Fatal("authorization lacks state/nonce/PKCE")
	}
	id, err := provider.Exchange(context.Background(), "code", "nonce", verifier)
	if err != nil || id.Subject != "subject" || !id.EmailVerified {
		t.Fatalf("verified identity: %+v %v", id, err)
	}
	for _, tc := range []struct {
		field string
		value any
	}{{"nonce", "wrong"}, {"aud", "other-client"}, {"iss", "https://evil.example"}, {"exp", time.Now().Add(-time.Minute).Unix()}} {
		before := claims[tc.field]
		claims[tc.field] = tc.value
		if _, err := provider.Exchange(context.Background(), "code", "nonce", verifier); err == nil {
			t.Fatalf("accepted invalid %s", tc.field)
		}
		claims[tc.field] = before
	}
	if _, err := New(context.Background(), "", "secret", "https://api.example.invalid/callback"); err == nil {
		t.Fatal("empty Google client accepted")
	}
	if _, err := New(context.Background(), "client", "secret", "http://evil.example/callback"); err == nil {
		t.Fatal("insecure redirect accepted")
	}
}
