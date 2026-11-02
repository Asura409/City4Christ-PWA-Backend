package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
)

// passwordInput delegates JSON syntax and string decoding to encoding/json, but
// rejects malformed Unicode instead of allowing the decoder to replace it.
// It is limited to password-changing input so existing API decoding is unchanged.
type passwordInput string

func (p *passwordInput) UnmarshalJSON(raw []byte) error {
	var decoded string
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	raw = bytes.TrimSpace(raw)
	if bytes.Equal(raw, []byte("null")) {
		return errors.New("password must be a JSON string")
	}
	if !utf8.Valid(raw) || !pairedPasswordSurrogates(raw) {
		return errors.New("password contains malformed Unicode")
	}
	*p = passwordInput(decoded)
	return nil
}

// JSON syntax has already been validated. Inspect only Unicode escape pairing;
// escaped backslashes are literal text and must not become Unicode escapes.
func pairedPasswordSurrogates(raw []byte) bool {
	for i := 1; i < len(raw)-1; {
		if raw[i] != '\\' {
			i++
			continue
		}
		if raw[i+1] != 'u' {
			i += 2
			continue
		}
		code, err := strconv.ParseUint(string(raw[i+2:i+6]), 16, 16)
		if err != nil {
			return false
		}
		i += 6
		if !utf16.IsSurrogate(rune(code)) {
			continue
		}
		if code > 0xDBFF || i+6 > len(raw)-1 || raw[i] != '\\' || raw[i+1] != 'u' {
			return false
		}
		low, err := strconv.ParseUint(string(raw[i+2:i+6]), 16, 16)
		if err != nil || utf16.DecodeRune(rune(code), rune(low)) == utf8.RuneError {
			return false
		}
		i += 6
	}
	return true
}
