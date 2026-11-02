package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPasswordInputPreservesValidUnicode(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
	}{
		{"literal Unicode", `"café 密码 😀"`, "café 密码 😀"},
		{"surrogate pair", `"before\uD83D\uDE00after"`, "before😀after"},
		{"lowercase pair", `"\ud83d\ude00"`, "😀"},
		{"boundary pairs", `"\uD800\uDC00\uDBFF\uDFFF"`, "\U00010000\U0010FFFF"},
		{"BMP escapes", `"\u0041\u00e9\uFFFF"`, "Aé\uFFFF"},
		{"literal replacement", `"�"`, "�"},
		{"escaped replacement", `"\uFFFD"`, "�"},
		{"escaped backslash", `"\\uD800"`, `\uD800`},
		{"escaped backslash low", `"\\uDC00"`, `\uDC00`},
		{"backslashes and valid pair", `"\\\uD83D\uDE00"`, "\\😀"},
		{"JSON slash and quotes", `"a\/b\"c\\d"`, "a/b\"c\\d"},
		{"spaces preserved", `"  a long password  "`, "  a long password  "},
		{"empty string", `""`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got passwordInput
			if err := json.Unmarshal([]byte(tc.input), &got); err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Fatal("valid password was altered")
			}
		})
	}
}

func TestPasswordInputRejectsMalformedUnicodeAndNonStrings(t *testing.T) {
	for _, tc := range []struct {
		name, input string
	}{
		{"invalid UTF-8 byte", "\"long password " + string([]byte{0xff}) + "\""},
		{"truncated UTF-8", "\"long password " + string([]byte{0xe2, 0x82}) + "\""},
		{"high surrogate", `"\uD800"`},
		{"low surrogate", `"\uDC00"`},
		{"high followed by BMP", `"\uD800\u0041"`},
		{"high followed by high", `"\uD800\uDBFF"`},
		{"low followed by high", `"\uDC00\uD800"`},
		{"high followed by text", `"\uD800abc"`},
		{"high followed by escaped slash", `"\uD800\\uDC00"`},
		{"pair then unpaired low", `"\uD83D\uDE00\uDC00"`},
		{"null", `null`},
		{"boolean", `true`},
		{"number", `123`},
		{"object", `{}`},
		{"array", `[]`},
		{"invalid escape", `"\x41"`},
		{"short Unicode escape", `"\u123"`},
		{"invalid hex", `"\uGGGG"`},
		{"unescaped newline", "\"before\nafter\""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := passwordInput("unchanged")
			if err := json.Unmarshal([]byte(tc.input), &got); err == nil {
				t.Fatal("malformed password input accepted")
			}
			if string(got) != "unchanged" {
				t.Fatal("invalid input changed the destination")
			}
		})
	}
}

func TestPasswordInputDirectDecoderRejectsWhitespaceNull(t *testing.T) {
	var got passwordInput
	if err := got.UnmarshalJSON([]byte(" \nnull\t")); err == nil {
		t.Fatal("null is not a password string")
	}
	if err := got.UnmarshalJSON([]byte(`"secret\uD800"`)); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("malformed Unicode error must not include password contents")
	}
}
