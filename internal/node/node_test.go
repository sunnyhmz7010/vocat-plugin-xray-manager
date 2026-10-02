package node

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

const testUUID = "12345678-1234-1234-1234-123456789abc"

func vmess(overrides map[string]any) string {
	m := map[string]any{"v": "2", "ps": "测试节点", "add": "example.com", "port": "443", "id": testUUID, "aid": "0", "net": "ws", "type": "none", "host": "cdn.example.com", "path": "/socket", "tls": "tls", "sni": "tls.example.com", "scy": "auto"}
	for k, v := range overrides {
		if v == nil {
			delete(m, k)
		} else {
			m[k] = v
		}
	}
	b, _ := json.Marshal(m)
	return "vmess://" + base64.RawStdEncoding.EncodeToString(b)
}
func nested(t *testing.T, value any, keys ...string) any {
	t.Helper()
	for _, key := range keys {
		m, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("expected map at %s", key)
		}
		value = m[key]
	}
	return value
}
func TestParseSupported(t *testing.T) {
	key := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	tests := []struct{ name, raw, protocol, network, security string }{
		{"vless plain", "vless://" + testUUID + "@example.com:80?encryption=none", "vless", "raw", "none"},
		{"vless IPv6 TLS", "vless://" + testUUID + "@[2001:db8::1]:443?security=tls&type=tcp&flow=xtls-rprx-vision&sni=tls.example.com&alpn=h2%2Chttp%2F1.1&fp=chrome#%E6%B5%8B%E8%AF%95", "vless", "raw", "tls"},
		{"vless Reality", "vless://" + testUUID + "@example.com:443?security=reality&pbk=" + key + "&sid=aabb&spx=%2Fsearch&sni=example.com", "vless", "raw", "reality"},
		{"vless WS", "vless://" + testUUID + "@example.com:443?security=tls&type=ws&host=cdn.example.com&path=%2Fws", "vless", "ws", "tls"},
		{"vless grpc", "vless://" + testUUID + "@example.com:443?security=tls&type=grpc&serviceName=svc&mode=multi", "vless", "grpc", "tls"},
		{"vless xhttp", "vless://" + testUUID + "@example.com:443?security=reality&pbk=" + key + "&type=xhttp&path=%2Fup&host=cdn.example.com&mode=packet-up", "vless", "xhttp", "reality"},
		{"trojan default TLS", "trojan://secret%3Awith%40symbols@example.com:443", "trojan", "raw", "tls"},
		{"trojan WS", "trojan://secret@example.com:443?type=ws&path=%2Fws&allowInsecure=false", "trojan", "ws", "tls"},
		{"vmess", "", "vmess", "ws", "tls"},
		{"ss plaintext", "ss://aes-128-gcm:secret%3Avalue@[::1]:8388#SS", "shadowsocks", "", ""},
		{"ss encoded", "ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:secret")) + "@example.com:8388/", "shadowsocks", "", ""},
	}
	tests[8].raw = vmess(nil)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, err := Parse(tt.raw)
			if err != nil {
				t.Fatal(err)
			}
			if n.Protocol != tt.protocol || n.Outbound["protocol"] != tt.protocol || n.Name == "" {
				t.Fatal("unexpected node metadata")
			}
			if tt.network != "" {
				if nested(t, n.Outbound, "streamSettings", "network") != tt.network || nested(t, n.Outbound, "streamSettings", "security") != tt.security {
					t.Fatal("unexpected transport")
				}
				if tt.security == "tls" && nested(t, n.Outbound, "streamSettings", "tlsSettings", "allowInsecure") != false {
					t.Fatal("TLS verification must be enabled")
				}
			}
			if _, err := json.Marshal(n.Outbound); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestExactMappings(t *testing.T) {
	keyBytes := make([]byte, 32)
	for i := range keyBytes {
		keyBytes[i] = 255
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)
	n, err := Parse("vless://" + testUUID + "@[2001:db8::1]:443?security=reality&pbk=" + url.QueryEscape(key) + "&sid=00ab&spx=%2Fa&fp=firefox&sni=example.com")
	if err != nil {
		t.Fatal(err)
	}
	reality := nested(t, n.Outbound, "streamSettings", "realitySettings").(map[string]any)
	expected := map[string]any{"password": base64.RawURLEncoding.EncodeToString(keyBytes), "shortId": "00ab", "spiderX": "/a", "fingerprint": "firefox", "serverName": "example.com"}
	if !reflect.DeepEqual(reality, expected) {
		t.Fatal("incorrect Reality mapping")
	}
	server := nested(t, n.Outbound, "settings", "vnext").([]any)[0].(map[string]any)
	if server["address"] != "2001:db8::1" || server["port"] != 443 {
		t.Fatal("incorrect IPv6 endpoint")
	}
	for _, network := range []string{"ws", "xhttp"} {
		n, err = Parse("vless://" + testUUID + "@example.com:443?type=" + network + "&path=%2Fa%3Fed%3D2048&host=cdn.example.com")
		if err != nil {
			t.Fatal(err)
		}
		settings := nested(t, n.Outbound, "streamSettings", network+"Settings").(map[string]any)
		if settings["path"] != "/a?ed=2048" || settings["host"] != "cdn.example.com" {
			t.Fatal("incorrect path/host")
		}
	}
	n, err = Parse("trojan://p%3Aa%40ss@example.com:443")
	if err != nil {
		t.Fatal(err)
	}
	if nested(t, n.Outbound, "settings", "servers").([]any)[0].(map[string]any)["password"] != "p:a@ss" {
		t.Fatal("credential not decoded exactly once")
	}
	n, err = Parse(vmess(map[string]any{"net": "grpc", "host": "", "path": "service", "port": 8443}))
	if err != nil {
		t.Fatal(err)
	}
	if nested(t, n.Outbound, "streamSettings", "grpcSettings", "serviceName") != "service" {
		t.Fatal("VMess serviceName mapping")
	}
}
func TestBase64Variants(t *testing.T) {
	encodings := []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding}
	for _, enc := range encodings {
		credentials := []byte("chacha20-ietf-poly1305:????>>>>")
		n, err := Parse("ss://" + enc.EncodeToString(credentials) + "@example.com:8388#name@node")
		if err != nil || n.Protocol != "shadowsocks" {
			t.Fatalf("SS encoding failed: %v", err)
		}
		raw := vmess(map[string]any{"ps": "中文????>>>>"})
		data, err := decode64(strings.TrimPrefix(raw, "vmess://"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = Parse("vmess://" + enc.EncodeToString(data)); err != nil {
			t.Fatal(err)
		}
		key := enc.EncodeToString(make([]byte, 32))
		if _, err = Parse("vless://" + testUUID + "@example.com:443?security=reality&pbk=" + url.QueryEscape(key)); err != nil {
			t.Fatal(err)
		}
	}
}
func TestRejectedAndNoSecretLeak(t *testing.T) {
	secret := "TOP_SECRET_DO_NOT_PRINT"
	base := "vless://" + testUUID + "@example.com:443"
	cases := map[string]string{
		"spaced query key":   base + "?type%20security=tls",
		"empty query key":    base + "?=value",
		"extra UUID hyphens": "vless://--345678-1234-1234-1234-123456789abc@example.com:443",
		"spaced VMess key":   vmess(map[string]any{"net type": "raw"}),
		"empty":              "", "too long": strings.Repeat("a", MaxLinkLength+1), "unknown protocol": "https://" + secret,
		"missing port": "trojan://" + secret + "@example.com", "zero port": "trojan://" + secret + "@example.com:0", "large port": "trojan://" + secret + "@example.com:65536", "bad port": "trojan://" + secret + "@example.com:bad", "negative port": "trojan://" + secret + "@example.com:-1", "unbracketed IPv6": "trojan://" + secret + "@2001:db8::1:443",
		"bad URL escape": "trojan://" + secret + "%xx@example.com:443", "bad query escape": base + "?sni=%xx", "bad UUID": "vless://" + secret + "@example.com:443", "UUID password": base[:len("vless://")] + testUUID + ":" + secret + "@example.com:443",
		"unknown query": base + "?" + secret + "=" + secret, "duplicate query": base + "?security=tls&security=none", "unknown transport": base + "?type=" + secret, "unknown security": base + "?security=" + secret,
		"insecure true": base + "?security=tls&allowInsecure=true", "insecure one": base + "?security=tls&allowInsecure=1", "insecure empty": base + "?allowInsecure", "encryption": base + "?encryption=" + secret, "empty encryption": base + "?encryption=", "flow": base + "?flow=" + secret,
		"flow ws": base + "?type=ws&security=tls&flow=xtls-rprx-vision", "flow plaintext": base + "?flow=xtls-rprx-vision", "raw path": base + "?path=/", "grpc host": base + "?type=grpc&host=x", "ws mode": base + "?type=ws&mode=gun", "xhttp mode": base + "?type=xhttp&mode=" + secret,
		"reality missing key": base + "?security=reality", "reality malformed key": base + "?security=reality&pbk=" + secret, "reality ws": base + "?security=reality&type=ws", "reality sid": base + "?security=reality&pbk=" + base64.RawURLEncoding.EncodeToString(make([]byte, 32)) + "&sid=abc",
		"tls reality key": base + "?security=tls&pbk=" + secret, "unused sni": base + "?sni=x", "bad fingerprint": base + "?security=tls&fp=" + secret, "empty ALPN": base + "?security=tls&alpn=h2,", "control name": base + "#a%0Ab", "control host query": base + "?type=ws&host=a%0Ab", "header": base + "?headerType=http",
		"ss plugin": "ss://aes-128-gcm:" + secret + "@example.com:8388?plugin=obfs-local", "ss unknown cipher": "ss://unknown:" + secret + "@example.com:8388", "ss empty password": "ss://aes-128-gcm:@example.com:8388", "ss old whole URL": "ss://" + base64.StdEncoding.EncodeToString([]byte("aes-128-gcm:"+secret+"@example.com:8388")), "ss2022 bad key": "ss://2022-blake3-aes-128-gcm:" + secret + "@example.com:8388",
		"vmess invalid base64": "vmess://" + secret, "vmess bad JSON": "vmess://" + base64.StdEncoding.EncodeToString([]byte(secret)), "vmess legacy aid": vmess(map[string]any{"aid": "1"}), "vmess invalid cipher": vmess(map[string]any{"scy": secret}), "vmess unknown field": vmess(map[string]any{secret: secret}), "vmess insecure": vmess(map[string]any{"allowInsecure": true}), "vmess decimal port": vmess(map[string]any{"port": 443.5}), "vmess nested field": vmess(map[string]any{"ps": map[string]any{"secret": secret}}), "vmess duplicate": "vmess://" + base64.RawStdEncoding.EncodeToString([]byte(`{"id":"`+secret+`","id":"`+secret+`"}`)),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			n, err := Parse(raw)
			if err == nil {
				t.Fatal("expected rejection")
			}
			if n.Outbound != nil || n.Name != "" || n.Protocol != "" {
				t.Fatal("partial node on error")
			}
			if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), testUUID) || (len(raw) > 10 && strings.Contains(err.Error(), raw)) {
				t.Fatal("error leaked input")
			}
		})
	}
}
func TestSS2022(t *testing.T) {
	for _, tt := range []struct {
		method string
		size   int
	}{{"2022-blake3-aes-128-gcm", 16}, {"2022-blake3-aes-256-gcm", 32}, {"2022-blake3-chacha20-poly1305", 32}} {
		key := base64.StdEncoding.EncodeToString(make([]byte, tt.size))
		raw := "ss://" + url.UserPassword(tt.method, key).String() + "@example.com:8388"
		if _, err := Parse(raw); err != nil {
			t.Fatal(err)
		}
	}
}
func FuzzParse(f *testing.F) {
	f.Add("vless://" + testUUID + "@example.com:443?security=tls")
	f.Add(vmess(nil))
	f.Add("ss://aes-128-gcm:pass@[::1]:8388")
	f.Fuzz(func(t *testing.T, raw string) {
		n, err := Parse(raw)
		if err != nil {
			if n.Outbound != nil {
				t.Fatal("partial result")
			}
			return
		}
		if _, err := json.Marshal(n.Outbound); err != nil {
			t.Fatal(err)
		}
	})
}
