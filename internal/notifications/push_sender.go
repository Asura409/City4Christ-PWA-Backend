package notifications

import (
	"context"
	"crypto/ecdh"
	"errors"
	webpush "github.com/SherClockHolmes/webpush-go"
	"net/http"
	"net/mail"
	"strings"
	"time"
)

type PushConfig struct {
	Enabled                        bool
	PublicKey, PrivateKey, Subject string
}

func ValidPushPublicKey(raw string) bool {
	b, e := decodePushKey(raw)
	if e != nil {
		return false
	}
	_, e = ecdh.P256().NewPublicKey(b)
	return e == nil
}
func LoadPushConfig(getenv func(string) string) (PushConfig, error) {
	c := PushConfig{}
	switch getenv("PUSH_ENABLED") {
	case "", "false":
		return c, nil
	case "true":
		c.Enabled = true
	default:
		return c, errors.New("PUSH_ENABLED must be true or false")
	}
	c.PublicKey = getenv("PUSH_VAPID_PUBLIC_KEY")
	c.PrivateKey = getenv("PUSH_VAPID_PRIVATE_KEY")
	c.Subject = getenv("PUSH_VAPID_SUBJECT")
	if !validPushConfig(c) {
		return PushConfig{}, errors.New("Web Push requires matching VAPID keys and a mailto contact")
	}
	return c, nil
}
func validPushConfig(c PushConfig) bool {
	private, e := decodePushKey(c.PrivateKey)
	if e != nil {
		return false
	}
	key, e := ecdh.P256().NewPrivateKey(private)
	if e != nil {
		return false
	}
	public, e := decodePushKey(c.PublicKey)
	if e != nil || string(public) != string(key.PublicKey().Bytes()) {
		return false
	}
	if !strings.HasPrefix(c.Subject, "mailto:") {
		return false
	}
	_, e = mail.ParseAddress(strings.TrimPrefix(c.Subject, "mailto:"))
	return e == nil
}

type VAPIDSender struct {
	config PushConfig
	client webpush.HTTPClient
}

func NewVAPIDSender(c PushConfig, client webpush.HTTPClient) (*VAPIDSender, error) {
	if !c.Enabled || !validPushConfig(c) {
		return nil, ErrInvalid
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	return &VAPIDSender{config: c, client: client}, nil
}
func (s *VAPIDSender) Send(ctx context.Context, sub PushSubscription, payload []byte) (int, error) {
	if !validSubscription(sub) || len(payload) > 2048 {
		return 0, ErrInvalid
	}
	response, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{Endpoint: sub.Endpoint, Keys: webpush.Keys{P256dh: sub.Keys.P256dh, Auth: sub.Keys.Auth}}, &webpush.Options{HTTPClient: s.client, Subscriber: s.config.Subject, VAPIDPublicKey: s.config.PublicKey, VAPIDPrivateKey: s.config.PrivateKey, TTL: 300, Topic: "city4christ-update"})
	if err != nil {
		return 0, errors.New("push provider request failed")
	}
	defer response.Body.Close()
	return response.StatusCode, nil
}
