package node

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestExpandedVLESSJSON(t *testing.T) {
	key := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	pq := base64.RawURLEncoding.EncodeToString(make([]byte, 1952))
	tests := []struct {
		name, query string
		path        []string
		want        any
	}{
		{"xudp", "packetEncoding=xudp", []string{"mux"}, map[string]any{"enabled": true, "concurrency": float64(-1), "xudpConcurrency": float64(16), "xudpProxyUDP443": "allow"}},
		{"raw http", "type=tcp&headerType=http&path=%2Fa,%2Fb&host=a.example,b.example", []string{"streamSettings", "rawSettings"}, map[string]any{"header": map[string]any{"type": "http", "request": map[string]any{"path": []any{"/a", "/b"}, "headers": map[string]any{"Host": []any{"a.example", "b.example"}}}}}},
		{"websocket ed", "type=websocket&path=%2Fw%3Ftoken%3Dabc&ed=2048&host=cdn.example", []string{"streamSettings", "wsSettings"}, map[string]any{"path": "/w?ed=2048&token=abc", "host": "cdn.example"}},
		{"httpupgrade ed", "type=httpupgrade&ed=1024", []string{"streamSettings", "httpupgradeSettings"}, map[string]any{"path": "/?ed=1024"}},
		{"grpc authority", "type=grpc&serviceName=svc&mode=multi&authority=cdn.example", []string{"streamSettings", "grpcSettings"}, map[string]any{"serviceName": "svc", "multiMode": true, "authority": "cdn.example"}},
		{"xhttp extra", "type=splithttp&mode=packet-up&extra=" + url.QueryEscape(`{"noGRPCHeader":true,"xPaddingBytes":"100-200","xmux":{"maxConcurrency":"1-4"}}`), []string{"streamSettings", "xhttpSettings"}, map[string]any{"mode": "packet-up", "extra": map[string]any{"noGRPCHeader": true, "xPaddingBytes": "100-200", "xmux": map[string]any{"maxConcurrency": "1-4"}}}},
		{"tls pin", "security=tls&pcs=" + strings.Repeat("ab", 32) + "&vcn=example.com,other.example&fp=hellochrome_131", []string{"streamSettings", "tlsSettings"}, map[string]any{"allowInsecure": false, "pinnedPeerCertSha256": strings.Repeat("ab", 32), "verifyPeerCertByName": "example.com,other.example", "fingerprint": "hellochrome_131"}},
		{"reality pqv", "security=reality&pbk=" + key + "&pqv=" + pq, []string{"streamSettings", "realitySettings"}, map[string]any{"password": key, "shortId": "", "fingerprint": "chrome", "mldsa65Verify": pq}},
		{"kcp", "type=mkcp", []string{"streamSettings"}, map[string]any{"network": "kcp", "security": "none", "kcpSettings": map[string]any{}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, err := Parse("vless://" + testUUID + "@example.com:443?" + tt.query)
			if err != nil {
				t.Fatal(err)
			}
			b, err := json.Marshal(n.Outbound)
			if err != nil {
				t.Fatal(err)
			}
			var actual map[string]any
			if err = json.Unmarshal(b, &actual); err != nil {
				t.Fatal(err)
			}
			if got := nested(t, actual, tt.path...); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("JSON mismatch: got %#v want %#v", got, tt.want)
			}
			checkXray(t, n)
		})
	}
	for _, mode := range []string{"native", "xorpub", "random"} {
		for _, rtt := range []string{"0rtt", "1rtt"} {
			encryption := "mlkem768x25519plus." + mode + "." + rtt + "." + key
			n, err := Parse("vless://" + testUUID + "@example.com:443?encryption=" + encryption)
			if err != nil {
				t.Fatal(err)
			}
			user := nested(t, n.Outbound, "settings", "vnext").([]any)[0].(map[string]any)["users"].([]any)[0].(map[string]any)
			if user["encryption"] != encryption {
				t.Fatal("encryption not preserved")
			}
			checkXray(t, n)
		}
	}
}

// 可选固定核心校验；run -test 不启动监听，也不拨号。
func checkXray(t *testing.T, n Node) {
	t.Helper()
	binary := os.Getenv("XRAY_TEST_BINARY")
	if binary == "" {
		return
	}
	config := map[string]any{"outbounds": []any{n.Outbound}}
	b, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err = os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(binary, "run", "-test", "-config", path).CombinedOutput(); err != nil {
		t.Fatalf("Xray config test failed: %v\n%s", err, output)
	}
}

func TestExpandedRejectsAndFieldNames(t *testing.T) {
	base := "vless://" + testUUID + "@example.com:443?"
	cases := []string{"type=ws&authority=x", "type=grpc&ed=1", "type=xhttp&ed=1", "type=httpupgrade&ed=-1", "type=ws&path=%2F%3Fed%3D1&ed=2", "type=httpupgrade&security=reality", "security=tls&pcs=1234", "security=tls&pcs=ab&pinSHA256=cd", "security=tls&verifyPeerCertInNames=example.com", "security=reality&pbk=" + base64.RawURLEncoding.EncodeToString(make([]byte, 32)) + "&fp=unsafe", "type=xhttp&extra=null", "type=xhttp&extra=" + url.QueryEscape(`{"outbounds":[]}`), "type=xhttp&extra=" + url.QueryEscape(`{"downloadSettings":{}}`), "type=xhttp&extra=" + url.QueryEscape(`{"headers":{"Host":"x"}}`), "encryption=mlkem768x25519plus.native.0rtt.abc", "packetEncoding=packet"}
	for _, q := range cases {
		if _, err := Parse(base + q); err == nil {
			t.Errorf("accepted %s", q)
		}
	}
	_, err := Parse(base + "unknownOption=PRIVATE_VALUE")
	if err == nil || !strings.Contains(err.Error(), "unknownOption") || strings.Contains(err.Error(), "PRIVATE_VALUE") {
		t.Fatal("unknown parameter must name field, never value")
	}
}
