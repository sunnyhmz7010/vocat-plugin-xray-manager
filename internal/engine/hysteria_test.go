package engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// 独立构造回环服务端，验证 QUIC/TLS、认证与混淆实际互通，不仅检查配置语法。
func TestHysteria2Forwarding(t *testing.T) {
	core := os.Getenv("XRAY_TEST_BINARY")
	if core == "" {
		t.Skip("设置 XRAY_TEST_BINARY 后执行真实内核测试")
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer origin.Close()
	fixture := httptest.NewTLSServer(http.NotFoundHandler())
	cert := fixture.TLS.Certificates[0]
	fixture.Close()
	key, err := x509.MarshalPKCS8PrivateKey(cert.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	certificate := strings.Split(strings.TrimSpace(string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}))), "\n")
	privateKey := strings.Split(strings.TrimSpace(string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}))), "\n")
	digest := sha256.Sum256(cert.Certificate[0])
	for _, obfs := range []bool{false, true} {
		t.Run(fmt.Sprintf("salamander=%v", obfs), func(t *testing.T) {
			port := testFreePort(t)
			stream := map[string]any{"network": "hysteria", "security": "tls", "hysteriaSettings": map[string]any{"version": 2}, "tlsSettings": map[string]any{"alpn": []string{"h3"}, "certificates": []any{map[string]any{"certificate": certificate, "key": privateKey}}}}
			if obfs {
				stream["finalmask"] = map[string]any{"udp": []any{map[string]any{"type": "salamander", "settings": map[string]any{"password": "mask-password"}}}}
			}
			server := map[string]any{"log": map[string]any{"loglevel": "none"}, "inbounds": []any{map[string]any{"listen": "127.0.0.1", "port": port, "protocol": "hysteria", "settings": map[string]any{"version": 2, "clients": []any{map[string]any{"auth": "user:pass@word"}}}, "streamSettings": stream}}, "outbounds": []any{map[string]any{"protocol": "freedom"}}}
			data, err := json.Marshal(server)
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(core, "run", "-config", "stdin:", "-format", "json")
			cmd.Stdin = bytes.NewReader(data)
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
			q := url.Values{"pinSHA256": {hex.EncodeToString(digest[:])}, "insecure": {"1"}, "sni": {"192.0.2.1"}}
			if obfs {
				q.Set("obfs", "salamander")
				q.Set("obfs-password", "mask-password")
			}
			link := fmt.Sprintf("hy2://user:pass%%40word@127.0.0.1:%d?%s", port, q.Encode())
			m := testManager(t, core)
			v, err := m.AddWithSettings(link, Settings{})
			if err != nil {
				t.Fatal(err)
			}
			for _, proxy := range []string{"socks5", "http"} {
				result, err := m.probeURL(context.Background(), v.ID, ProbeOptions{ProxyProtocol: proxy, TimeoutSeconds: 5}, origin.URL)
				if err != nil || !result.OK || result.StatusCode != http.StatusNoContent {
					t.Fatalf("%s forwarding: %+v %v", proxy, result, err)
				}
			}
			if _, err := m.Export(v.ID); err != nil {
				t.Fatal(err)
			}
			if stored, err := m.Connection(v.ID); err != nil || stored != link {
				t.Fatal("原分享链接被改写", err)
			}
			// insecure=1 不能绕过指纹校验，即使格式正确但证书不匹配也必须失败。
			wrongPin := strings.Replace(link, hex.EncodeToString(digest[:]), strings.Repeat("00", 32), 1)
			if _, err := m.SetConnection(v.ID, wrongPin); err != nil {
				t.Fatal(err)
			}
			pinResult, err := m.probeURL(context.Background(), v.ID, ProbeOptions{TimeoutSeconds: 2}, origin.URL)
			if err != nil || pinResult.OK {
				t.Fatalf("wrong certificate pin succeeded: %+v %v", pinResult, err)
			}
			// 错误认证不能直连回退；编辑链接仍保留节点身份和端口。
			updated, err := m.SetConnection(v.ID, strings.Replace(link, "user:pass%40word", "wrong-password", 1))
			if err != nil || updated.ID != v.ID || updated.Addr != v.Addr {
				t.Fatalf("edit: %+v %v", updated, err)
			}
			failed, err := m.probeURL(context.Background(), v.ID, ProbeOptions{TimeoutSeconds: 2}, origin.URL)
			if err != nil || failed.OK {
				t.Fatalf("wrong auth succeeded: %+v %v", failed, err)
			}
			if _, err := m.SetConnection(v.ID, link); err != nil {
				t.Fatal(err)
			}
			if _, err := m.SetEnabled(v.ID, false); err != nil {
				t.Fatal(err)
			}
			if _, err := m.SetEnabled(v.ID, true); err != nil {
				t.Fatal(err)
			}
			result, err := m.probeURL(context.Background(), v.ID, ProbeOptions{TimeoutSeconds: 5}, origin.URL)
			if err != nil || !result.OK {
				t.Fatalf("restart: %+v %v", result, err)
			}
		})
	}
}
