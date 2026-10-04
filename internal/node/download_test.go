package node

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func downloadJSON(t *testing.T, raw string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestValidateDownloadSettings(t *testing.T) {
	key := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	pq := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{2}, 1952))
	valid := []string{
		`{"address":"127.0.0.1","port":8080,"network":"xhttp"}`,
		`{"address":"::1","port":65535,"network":"splithttp","security":"none","splithttpSettings":{"host":"download.test","path":"/down?q=1","extra":{"noSSEHeader":true,"xmux":{"maxConnections":1}}}}`,
		`{"address":"download.test","port":443,"network":"xhttp","security":"tls","tlsSettings":{"serverName":"download.test","fingerprint":"chrome","alpn":["h2"],"pinnedPeerCertSha256":"` + strings.Repeat("ab", 32) + `","verifyPeerCertByName":"download.test,other.test","allowInsecure":false},"xhttpSettings":{"host":"cdn.test","path":"/download","noGRPCHeader":true}}`,
		`{"address":"download.test","port":443,"network":"xhttp","security":"tls"}`,
		`{"address":"download.test","port":443,"network":"xhttp","security":"reality","realitySettings":{"password":"` + key + `","shortId":"0123","serverName":"download.test","mldsa65Verify":"` + pq + `","spiderX":"/"}}`,
	}
	for i, raw := range valid {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			m := downloadJSON(t, raw)
			before := downloadJSON(t, raw)
			if err := validateDownloadSettings(m); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(m, before) {
				t.Fatal("validation mutated input")
			}
		})
	}
	invalid := map[string]string{
		"missing address": `{"port":443,"network":"xhttp"}`,
		"missing port":    `{"address":"127.0.0.1","network":"xhttp"}`,
		"missing network": `{"address":"127.0.0.1","port":443}`,
		"zero port":       `{"port":0}`, "negative port": `{"port":-1}`, "large port": `{"port":65536}`, "fraction port": `{"port":1.5}`, "string port": `{"port":"443"}`, "null port": `{"port":null}`,
		"raw": `{"network":"raw"}`, "ws": `{"network":"ws"}`, "null network": `{"network":null}`,
		"address type": `{"address":42}`, "empty address": `{"address":""}`, "address url": `{"address":"https://localhost"}`,
		"sockopt": `{"sockopt":{"penetrate":true}}`, "penetrate": `{"penetrate":true}`, "outbound": `{"outbound":{}}`, "proxySettings": `{"proxySettings":{}}`,
		"security type": `{"security":false}`, "security unknown": `{"security":"xtls"}`, "security mismatch": `{"tlsSettings":{}}`, "reality missing": `{"security":"reality"}`,
		"settings null": `{"xhttpSettings":null}`, "aliases": `{"xhttpSettings":{},"splithttpSettings":{}}`,
		"recursive direct": `{"xhttpSettings":{"downloadSettings":{}}}`, "recursive extra": `{"xhttpSettings":{"extra":{"downloadSettings":{}}}}`,
		"extra unknown": `{"xhttpSettings":{"extra":{"outbound":{}}}}`, "extra type": `{"xhttpSettings":{"extra":"{}"}}`,
		"discarded direct": `{"xhttpSettings":{"noSSEHeader":true,"extra":{}}}`, "ignored mode": `{"xhttpSettings":{"mode":"stream-one"}}`, "host type": `{"xhttpSettings":{"host":false}}`,
		"local certificate": `{"security":"tls","tlsSettings":{"certificates":[{"certificateFile":"C:/secret"}]}}`,
		"key log":           `{"security":"tls","tlsSettings":{"masterKeyLog":"C:/secret"}}`,
		"ech sockopt":       `{"security":"tls","tlsSettings":{"echSockopt":{}}}`,
		"insecure":          `{"security":"tls","tlsSettings":{"allowInsecure":true}}`, "tls null": `{"security":"tls","tlsSettings":null}`,
		"alpn string": `{"security":"tls","tlsSettings":{"alpn":"h2"}}`, "alpn unsupported": `{"security":"tls","tlsSettings":{"alpn":["fromMitm"]}}`,
		"bad fingerprint": `{"security":"tls","tlsSettings":{"fingerprint":"bogus"}}`, "bad pin": `{"security":"tls","tlsSettings":{"pinnedPeerCertSha256":"abc"}}`,
		"empty name":     `{"security":"tls","tlsSettings":{"verifyPeerCertByName":"a,,b"}}`,
		"bad key":        `{"security":"reality","realitySettings":{"password":"short"}}`,
		"both keys":      `{"security":"reality","realitySettings":{"password":"` + key + `","publicKey":"` + key + `"}}`,
		"bad shortid":    `{"security":"reality","realitySettings":{"publicKey":"` + key + `","shortId":"f"}}`,
		"bad pq":         `{"security":"reality","realitySettings":{"publicKey":"` + key + `","mldsa65Verify":"short"}}`,
		"bad spider":     `{"security":"reality","realitySettings":{"publicKey":"` + key + `","spiderX":"/%zz"}}`,
		"reality server": `{"security":"reality","realitySettings":{"publicKey":"` + key + `","target":"127.0.0.1:443"}}`,
		"long host":      `{"xhttpSettings":{"host":"` + strings.Repeat("a", 254) + `"}}`,
		"long path":      `{"xhttpSettings":{"path":"` + strings.Repeat("a", 4097) + `"}}`,
	}
	for name, patch := range invalid {
		t.Run(name, func(t *testing.T) {
			m := downloadJSON(t, `{"address":"127.0.0.1","port":443,"network":"xhttp"}`)
			if strings.HasPrefix(name, "missing ") {
				m = downloadJSON(t, patch)
			} else {
				for k, v := range downloadJSON(t, patch) {
					m[k] = v
				}
			}
			before, _ := json.Marshal(m)
			if err := validateDownloadSettings(m); err == nil {
				t.Fatal("accepted invalid settings")
			}
			after, _ := json.Marshal(m)
			if !bytes.Equal(before, after) {
				t.Fatal("rejected validation mutated input")
			}
		})
	}
	for _, v := range []any{nil, "{}", []any{}, map[string]any(nil)} {
		if validateDownloadSettings(v) == nil {
			t.Fatalf("accepted %T", v)
		}
	}
}

func downloadCoreBinary(t *testing.T) string {
	t.Helper()
	binary := os.Getenv("XRAY_TEST_BINARY")
	if binary == "" {
		binary = filepath.Join("..", "..", "..", "vocat-plugin-node-proxy", ".cache", "test-bin", "xray.exe")
	}
	absolute, err := filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(absolute); err != nil {
		if os.Getenv("XRAY_TEST_BINARY") != "" {
			t.Fatal(err)
		}
		t.Skip("设置 XRAY_TEST_BINARY 以运行固定核心测试")
	}
	out, err := exec.Command(absolute, "version").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "Xray 26.3.27") {
		t.Fatalf("need Xray 26.3.27: %s (%v)", out, err)
	}
	return absolute
}

func downloadWriteConfig(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDownloadSettingsCoreConfig(t *testing.T) {
	binary := downloadCoreBinary(t)
	key := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	pq := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{2}, 1952))
	for name, suffix := range map[string]string{
		"plain":   ``,
		"tls":     `,"security":"tls","tlsSettings":{"fingerprint":"chrome","alpn":["h2"],"serverName":"localhost","pinnedPeerCertSha256":"` + strings.Repeat("ab", 32) + `","verifyPeerCertByName":"localhost"}`,
		"reality": `,"security":"reality","realitySettings":{"publicKey":"` + key + `","shortId":"0123","mldsa65Verify":"` + pq + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			download := downloadJSON(t, `{"address":"127.0.0.1","port":14443,"network":"xhttp","xhttpSettings":{"host":"localhost","path":"/down","extra":{"noSSEHeader":true}}`+suffix+`}`)
			if err := validateDownloadSettings(download); err != nil {
				t.Fatal(err)
			}
			config := map[string]any{"outbounds": []any{downloadOutbound(14444, download)}}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			out, err := exec.CommandContext(ctx, binary, "run", "-test", "-config", downloadWriteConfig(t, config)).CombinedOutput()
			if err != nil {
				t.Fatalf("core rejected: %v\n%s", err, out)
			}
		})
	}
}

const downloadTestUUID = "c14aa472-d3d4-4a6d-b08f-06c532486b18"

func downloadOutbound(port int, download map[string]any) map[string]any {
	return map[string]any{"protocol": "vless", "settings": map[string]any{"vnext": []any{map[string]any{"address": "127.0.0.1", "port": port, "users": []any{map[string]any{"id": downloadTestUUID, "encryption": "none"}}}}}, "streamSettings": map[string]any{"network": "xhttp", "xhttpSettings": map[string]any{"host": "upload.test", "path": "/upload", "mode": "packet-up", "extra": map[string]any{"downloadSettings": download}}}}
}

func downloadUnusedPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return p
}

func downloadStartCore(t *testing.T, binary string, config any, port int) {
	t.Helper()
	// 文件日志避免并发读取进程输出造成 race；进程在测试结束时始终回收。
	log, err := os.Create(filepath.Join(t.TempDir(), "core.log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "run", "-config", downloadWriteConfig(t, config))
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		log.Close()
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-done
		log.Close()
		if t.Failed() {
			b, _ := os.ReadFile(log.Name())
			t.Log(string(b))
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("core did not start loopback listener")
}

// 两个独立入口必须汇聚到同一个 XHTTP listener：核心的 sessions 是每个 handler
// 私有的，两个互不相通的核心服务端不能拼接同一会话。测试不连接任何公网目标。
func TestDownloadSettingsLoopback(t *testing.T) {
	binary := downloadCoreBinary(t)
	for _, secure := range []bool{false, true} {
		t.Run(fmt.Sprintf("downloadTLS=%v", secure), func(t *testing.T) {
			backendPort := downloadUnusedPort(t)
			backend := map[string]any{"log": map[string]any{"loglevel": "warning"}, "inbounds": []any{map[string]any{"listen": "127.0.0.1", "port": backendPort, "protocol": "vless", "settings": map[string]any{"clients": []any{map[string]any{"id": downloadTestUUID}}, "decryption": "none"}, "streamSettings": map[string]any{"network": "xhttp", "xhttpSettings": map[string]any{"path": "/shared"}}}}, "outbounds": []any{map[string]any{"protocol": "freedom"}}}
			downloadStartCore(t, binary, backend, backendPort)
			target, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", backendPort))
			var uploads, downloads atomic.Int64
			frontend := func(prefix, host string, counter *atomic.Int64, tls bool) *httptest.Server {
				proxy := httputil.NewSingleHostReverseProxy(target)
				proxy.FlushInterval = -1
				director := proxy.Director
				proxy.Director = func(r *http.Request) { director(r); r.URL.Path = "/shared" + strings.TrimPrefix(r.URL.Path, prefix) }
				s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Host != host || !strings.HasPrefix(r.URL.Path, prefix+"/") {
						http.Error(w, "unexpected host/path", 400)
						return
					}
					if (prefix == "/download" && r.Method != "GET") || (prefix == "/upload" && r.Method != "POST") {
						http.Error(w, "unexpected direction", 400)
						return
					}
					counter.Add(1)
					proxy.ServeHTTP(w, r)
				}))
				if tls {
					s.EnableHTTP2 = true
					s.StartTLS()
				} else {
					s.Start()
				}
				t.Cleanup(s.Close)
				return s
			}
			up := frontend("/upload", "upload.test", &uploads, false)
			down := frontend("/download", "download.test", &downloads, secure)
			upPort := up.Listener.Addr().(*net.TCPAddr).Port
			downPort := down.Listener.Addr().(*net.TCPAddr).Port
			download := downloadJSON(t, fmt.Sprintf(`{"address":"127.0.0.1","port":%d,"network":"xhttp","xhttpSettings":{"host":"download.test","path":"/download"}}`, downPort))
			if secure {
				pin := sha256.Sum256(down.Certificate().Raw)
				download["security"] = "tls"
				download["tlsSettings"] = map[string]any{"serverName": "example.com", "fingerprint": "chrome", "alpn": []any{"h2"}, "pinnedPeerCertSha256": hex.EncodeToString(pin[:])}
			}
			if err := validateDownloadSettings(download); err != nil {
				t.Fatal(err)
			}
			clientPort := downloadUnusedPort(t)
			clientConfig := map[string]any{"log": map[string]any{"loglevel": "warning"}, "inbounds": []any{map[string]any{"listen": "127.0.0.1", "port": clientPort, "protocol": "http", "settings": map[string]any{}}}, "outbounds": []any{downloadOutbound(upPort, download)}}
			downloadStartCore(t, binary, clientConfig, clientPort)
			payload := bytes.Repeat([]byte("loopback-xhttp-upload-download\n"), 4096)
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
				if !bytes.Equal(b, payload) {
					http.Error(w, "payload mismatch", 400)
					return
				}
				w.Write(payload)
			}))
			defer origin.Close()
			proxyURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", clientPort))
			transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: 15 * time.Second}
			resp, err := client.Post(origin.URL, "application/octet-stream", bytes.NewReader(payload))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil || resp.StatusCode != 200 || !bytes.Equal(body, payload) {
				t.Fatalf("round trip failed: status=%d bytes=%d err=%v", resp.StatusCode, len(body), err)
			}
			if uploads.Load() == 0 || downloads.Load() == 0 {
				t.Fatalf("both frontends must carry traffic: up=%d down=%d", uploads.Load(), downloads.Load())
			}
		})
	}
}
