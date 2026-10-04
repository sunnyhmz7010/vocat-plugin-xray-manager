package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"testing"
)

// 服务端直接使用已核实的finalmask，不复用客户端解析结果，避免两端同时映射错误。
func TestKCPSeedAndHeaderForwarding(t *testing.T) {
	core := os.Getenv("XRAY_TEST_BINARY")
	if core == "" {
		t.Skip("设置 XRAY_TEST_BINARY 后执行真实核心测试")
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "kcp-forwarded") }))
	defer origin.Close()
	for _, header := range []struct{ query, mask string }{{"none", ""}, {"srtp", "header-srtp"}, {"utp", "header-utp"}, {"wechat-video", "header-wechat"}, {"dtls", "header-dtls"}, {"wireguard", "header-wireguard"}} {
		for _, seeded := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/seed=%v", header.query, seeded), func(t *testing.T) {
				port := testFreePort(t)
				layers := []any{}
				if header.mask != "" {
					layers = append(layers, map[string]any{"type": header.mask})
				}
				if seeded {
					layers = append(layers, map[string]any{"type": "mkcp-aes128gcm", "settings": map[string]any{"password": "matching-seed"}})
				} else {
					layers = append(layers, map[string]any{"type": "mkcp-original"})
				}
				stream := map[string]any{"network": "kcp", "security": "none", "kcpSettings": map[string]any{}, "finalmask": map[string]any{"udp": layers}}
				server := map[string]any{"log": map[string]any{"loglevel": "none"}, "inbounds": []any{map[string]any{"listen": "127.0.0.1", "port": port, "protocol": "vless", "settings": map[string]any{"clients": []any{map[string]string{"id": testID}}, "decryption": "none"}, "streamSettings": stream}}, "outbounds": []any{map[string]any{"protocol": "freedom"}}}
				data, _ := json.Marshal(server)
				cmd := exec.Command(core, "run", "-config", "stdin:", "-format", "json")
				cmd.Stdin = bytes.NewReader(data)
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
				q := url.Values{"type": {"kcp"}, "headerType": {header.query}}
				if seeded {
					q.Set("seed", "matching-seed")
				}
				link := fmt.Sprintf("vless://%s@127.0.0.1:%d?%s", testID, port, q.Encode())
				m := testManager(t, core)
				v, err := m.AddWithSettings(link, Settings{})
				if err != nil {
					t.Fatal(err)
				}
				result, err := m.probeURL(context.Background(), v.ID, ProbeOptions{ProxyProtocol: "socks5", TimeoutSeconds: 5}, origin.URL)
				if err != nil || !result.OK || result.StatusCode != 200 {
					t.Fatalf("KCP forwarding failed: %+v %v", result, err)
				}
				if seeded && header.query == "srtp" {
					q.Set("seed", "wrong-seed")
					wrongLink := fmt.Sprintf("vless://%s@127.0.0.1:%d?%s", testID, port, q.Encode())
					wrong, err := m.AddWithSettings(wrongLink, Settings{})
					if err != nil {
						t.Fatal(err)
					}
					failed, err := m.probeURL(context.Background(), wrong.ID, ProbeOptions{TimeoutSeconds: 1}, origin.URL)
					if err != nil || failed.OK {
						t.Fatalf("wrong seed succeeded: %+v %v", failed, err)
					}
				}
			})
		}
	}
}
