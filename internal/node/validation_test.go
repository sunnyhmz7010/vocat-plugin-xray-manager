package node

import (
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
)

func TestEncryptionKeysAndFingerprints(t *testing.T) {
	base := "vless://" + testUUID + "@example.com:443?"
	key := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	mlkem := base64.RawURLEncoding.EncodeToString(make([]byte, 1184))
	for _, keys := range []string{mlkem, key + "." + mlkem, "100-100-200.50-0-10." + key} {
		n, err := Parse(base + "encryption=mlkem768x25519plus.native.1rtt." + keys)
		if err != nil {
			t.Fatal(err)
		}
		checkXray(t, n)
	}
	for _, fp := range strings.Fields(fingerprints) {
		n, err := Parse(base + "security=tls&fp=" + fp)
		if err != nil {
			t.Fatal(fp, err)
		}
		checkXray(t, n)
		_, err = Parse(base + "security=reality&pbk=" + key + "&fp=" + fp)
		rejected := fp == "unsafe" || fp == "hellogolang"
		if (err != nil) != rejected {
			t.Fatalf("Reality fingerprint %s: %v", fp, err)
		}
	}
}

func TestExtraValidation(t *testing.T) {
	for _, extra := range []string{
		`{"noGRPCHeader":true,"noGRPCHeader":false}`,
		`{"noGRPCHeader":"true"}`, `{"xPaddingKey":42}`,
		`{"xmux":{"maxConcurrency":"4-1"}}`,
		`{"xmux":{"maxConcurrency":1,"maxConcurrency":2}}`,
		`{"serverMaxHeaderBytes":-1}`, `{"scMinPostsIntervalMs":[]}`,
		`{"headers":{"x":"a\nb"}}`, `{"headers":{"x":42}}`,
	} {
		if _, err := Parse("vless://" + testUUID + "@example.com:443?type=xhttp&extra=" + url.QueryEscape(extra)); err == nil {
			t.Errorf("accepted invalid extra: %s", extra)
		}
	}
}
