package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/visitors"
	"os"
)

func loadVisitorConfig(getenv func(string) string) (bool, *visitors.Protection, error) {
	enabled := false
	switch getenv("VISITOR_INTAKE_ENABLED") {
	case "", "false":
	case "true":
		enabled = true
	default:
		return false, nil, errors.New("VISITOR_INTAKE_ENABLED must be true or false")
	}
	id, encodedCurrent, rawPrevious := getenv("VISITOR_DATA_KEY_ID"), getenv("VISITOR_DATA_KEY_BASE64"), getenv("VISITOR_DATA_PREVIOUS_KEYS_JSON")
	if id == "" && encodedCurrent == "" && rawPrevious == "" && !enabled {
		return false, nil, nil
	}
	current, e := base64.StdEncoding.DecodeString(encodedCurrent)
	if e != nil || len(current) != 32 {
		return false, nil, errors.New("visitor contact encryption requires a dedicated 32-byte key")
	}
	keys := map[string][]byte{id: current}
	previous := map[string]string{}
	if rawPrevious != "" && json.Unmarshal([]byte(rawPrevious), &previous) != nil {
		return false, nil, errors.New("previous visitor key configuration is invalid")
	}
	if len(previous) > 7 {
		return false, nil, errors.New("too many previous visitor keys")
	}
	for k, v := range previous {
		if k == id {
			return false, nil, errors.New("previous visitor key IDs must differ from the current ID")
		}
		key, e := base64.StdEncoding.DecodeString(v)
		if e != nil || len(key) != 32 {
			return false, nil, errors.New("visitor keyring must contain 32-byte keys")
		}
		keys[k] = key
	}
	p, e := visitors.NewProtection(id, keys)
	if e != nil {
		return false, nil, errors.New("visitor keyring configuration is invalid")
	}
	return enabled, p, nil
}
func generateVisitorKeys(path string) error {
	key := make([]byte, 32)
	if _, e := rand.Read(key); e != nil {
		return errors.New("visitor key generation failed")
	}
	encoded := base64.StdEncoding.EncodeToString(key)
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return errors.New("create a new private key file; existing files are never overwritten")
	}
	defer f.Close()
	_, e = fmt.Fprintf(f, "VISITOR_INTAKE_ENABLED=false\nVISITOR_DATA_KEY_ID=v1\nVISITOR_DATA_KEY_BASE64=%s\nVISITOR_DATA_PREVIOUS_KEYS_JSON=\n", encoded)
	return e
}
