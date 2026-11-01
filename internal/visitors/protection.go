package visitors

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"regexp"
)

type dataKey struct {
	cipher cipher.AEAD
	digest []byte
}
type Protection struct {
	current string
	keys    map[string]dataKey
}

var keyIDFormat = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

func NewProtection(current string, keys map[string][]byte) (*Protection, error) {
	if !keyIDFormat.MatchString(current) || len(keys) < 1 || len(keys) > 8 {
		return nil, ErrUnavailable
	}
	p := &Protection{current: current, keys: map[string]dataKey{}}
	for id, root := range keys {
		if !keyIDFormat.MatchString(id) || len(root) != 32 {
			return nil, ErrUnavailable
		}
		enc, e := hkdf.Key(sha256.New, root, nil, "city4christ/visitor-contact/v1", 32)
		if e != nil {
			return nil, ErrUnavailable
		}
		digest, e := hkdf.Key(sha256.New, root, nil, "city4christ/visitor-request/v1", 32)
		if e != nil {
			return nil, ErrUnavailable
		}
		block, e := aes.NewCipher(enc)
		if e != nil {
			return nil, ErrUnavailable
		}
		aead, e := cipher.NewGCMWithRandomNonce(block)
		if e != nil {
			return nil, ErrUnavailable
		}
		p.keys[id] = dataKey{aead, digest}
	}
	if _, ok := p.keys[current]; !ok {
		return nil, ErrUnavailable
	}
	return p, nil
}
func (p *Protection) requestDigest(key string, in Submission) ([]byte, error) {
	k, ok := p.keys[key]
	if !ok {
		return nil, ErrUnavailable
	}
	body, e := json.Marshal(in)
	if e != nil {
		return nil, ErrUnavailable
	}
	mac := hmac.New(sha256.New, k.digest)
	_, _ = mac.Write(body)
	return mac.Sum(nil), nil
}
func (p *Protection) seal(id string, in Submission) ([]byte, error) {
	body, e := json.Marshal(contact{in.FullName, in.Email, in.Phone})
	if e != nil {
		return nil, ErrUnavailable
	}
	return p.keys[p.current].cipher.Seal(nil, nil, body, []byte("visitor-contact:v1:"+id+":"+p.current)), nil
}
func (p *Protection) open(id, key string, sealed []byte) (contact, error) {
	k, ok := p.keys[key]
	if !ok {
		return contact{}, ErrUnavailable
	}
	body, e := k.cipher.Open(nil, nil, sealed, []byte("visitor-contact:v1:"+id+":"+key))
	if e != nil {
		return contact{}, ErrUnavailable
	}
	var c contact
	if json.Unmarshal(body, &c) != nil {
		return contact{}, ErrUnavailable
	}
	return c, nil
}
