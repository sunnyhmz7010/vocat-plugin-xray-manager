package engine

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func probeManager(t *testing.T, address, mode string, auth bool) *Manager {
	t.Helper()
	_, portText, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	return &Manager{
		records:   []Record{{ID: "test", Link: "secret-node-link", Port: port, InboundMode: mode, AuthEnabled: auth, Username: "probe-user", Password: "p@ss:/?#word"}},
		processes: map[string]*process{"test": {done: make(chan struct{})}},
	}
}

func TestProbeHTTPProxy(t *testing.T) {
	for _, mode := range []string{"", "mixed", "http"} {
		t.Run(mode, func(t *testing.T) {
			var requests atomic.Int32
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.URL.String() != "http://target.invalid/check?x=1" {
					t.Errorf("proxy URL = %s", r.URL)
				}
				want := "Basic " + base64.StdEncoding.EncodeToString([]byte("probe-user:p@ss:/?#word"))
				if r.Header.Get("Proxy-Authorization") != want {
					t.Error("missing proxy credentials")
				}
				if r.Header.Get("Authorization") != "" {
					t.Error("proxy credentials leaked to origin authorization")
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer proxy.Close()
			t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
			t.Setenv("NO_PROXY", "*")
			m := probeManager(t, proxy.Listener.Addr().String(), mode, true)
			result, err := m.probeURL(context.Background(), "test", ProbeOptions{}, "http://target.invalid/check?x=1")
			if err != nil || !result.OK || result.StatusCode != 204 || requests.Load() != 1 {
				t.Fatalf("result=%+v err=%v requests=%d", result, err, requests.Load())
			}
		})
	}
}

func TestProbeTargetStatusAndRedirect(t *testing.T) {
	var direct atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { direct.Add(1) }))
	defer target.Close()
	for _, status := range []int{200, 302, 404, 503} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			var calls atomic.Int32
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Header.Get("Proxy-Authorization") != "" {
					t.Error("unexpected authentication")
				}
				w.Header().Set("Location", target.URL)
				w.WriteHeader(status)
			}))
			defer proxy.Close()
			m := probeManager(t, proxy.Listener.Addr().String(), "http", false)
			result, err := m.probeURL(context.Background(), "test", ProbeOptions{}, "http://target.invalid/")
			if err != nil || result.StatusCode != status || result.OK != (status < 400) || calls.Load() != 1 {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if status >= 400 && result.Error != fmt.Sprintf("代理请求返回 HTTP %d", status) {
				t.Fatalf("wrong status explanation: %+v", result)
			}
		})
	}
	if direct.Load() != 0 {
		t.Fatal("redirect followed")
	}
}

func TestProbeValidationAndState(t *testing.T) {
	m := probeManager(t, "127.0.0.1:1", "http", false)
	for _, target := range []string{"custom", "http://example.com", "https://127.0.0.1/", "localhost"} {
		if _, err := m.Probe(context.Background(), "test", ProbeOptions{Target: target}); err == nil {
			t.Errorf("accepted probe target %q", target)
		}
	}
	for _, opts := range []ProbeOptions{{Target: "google", TimeoutSeconds: -1}, {Target: "google", TimeoutSeconds: 31}, {Target: "google", Method: "POST"}} {
		if _, err := m.Probe(context.Background(), "test", opts); err == nil {
			t.Errorf("accepted options %+v", opts)
		}
	}
	for target, want := range map[string]string{
		"":           "https://www.gstatic.com/generate_204",
		"google":     "https://www.gstatic.com/generate_204",
		"cloudflare": "https://cp.cloudflare.com/generate_204",
	} {
		got, err := probeTargetURL(target)
		if err != nil || got != want {
			t.Fatalf("target %q = %q, %v; want %q", target, got, err, want)
		}
	}
	opts := ProbeOptions{Target: "google"}
	if _, err := m.Probe(context.Background(), "missing", opts); err == nil {
		t.Fatal("accepted missing node")
	}
	close(m.processes["test"].done)
	if _, err := m.Probe(context.Background(), "test", opts); err == nil {
		t.Fatal("accepted stopped node")
	}
	m.closed = true
	if _, err := m.Probe(context.Background(), "test", opts); err == nil {
		t.Fatal("accepted closed manager")
	}
}
func TestProbeNoDirectFallback(t *testing.T) {
	var direct atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { direct.Add(1) }))
	defer target.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	for _, mode := range []string{"http", "socks5"} {
		m := probeManager(t, address, "mixed", true)
		result, err := m.probeURL(context.Background(), "test", ProbeOptions{TimeoutSeconds: 1, ProxyProtocol: mode}, target.URL)
		if err != nil || result.OK || result.StatusCode != 0 || result.Error == "" {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		if strings.Contains(result.Error, m.records[0].Password) || strings.Contains(result.Error, m.records[0].Link) {
			t.Fatal("secret leaked")
		}
	}
	if direct.Load() != 0 {
		t.Fatal("direct fallback reached target")
	}
}

func TestProbeTimeoutBodyAndUnlockedManager(t *testing.T) {
	started := make(chan struct{})
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer proxy.Close()
	m := probeManager(t, proxy.Listener.Addr().String(), "http", false)
	resultCh := make(chan ProbeResult, 1)
	go func() {
		result, err := m.probeURL(context.Background(), "test", ProbeOptions{TimeoutSeconds: 1}, "http://target.invalid")
		if err != nil {
			t.Error(err)
		}
		resultCh <- result
	}()
	<-started
	unlocked := make(chan struct{})
	go func() { m.mu.Lock(); m.mu.Unlock(); close(unlocked) }()
	select {
	case <-unlocked:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("network request holds manager lock")
	}
	result := <-resultCh
	if result.OK || result.Error != "探测超时" || result.StatusCode != 200 {
		t.Fatalf("result=%+v", result)
	}
}

func TestProbeCancellation(t *testing.T) {
	started := make(chan struct{})
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() }))
	defer proxy.Close()
	m := probeManager(t, proxy.Listener.Addr().String(), "http", false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { <-started; cancel() }()
	result, err := m.probeURL(ctx, "test", ProbeOptions{}, "http://target.invalid")
	if err != nil || result.OK || result.Error != "探测已取消" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestProbeTLSCertificateValidation(t *testing.T) {
	var originRequests atomic.Int32
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { originRequests.Add(1) }))
	defer origin.Close()
	var connects atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || r.Host != origin.Listener.Addr().String() {
			t.Errorf("unexpected CONNECT target: %s %s", r.Method, r.Host)
			w.WriteHeader(400)
			return
		}
		want := "Basic " + base64.StdEncoding.EncodeToString([]byte("probe-user:p@ss:/?#word"))
		if r.Header.Get("Proxy-Authorization") != want {
			t.Error("CONNECT missing credentials")
		}
		connects.Add(1)
		upstream, err := net.Dial("tcp", origin.Listener.Addr().String())
		if err != nil {
			t.Error(err)
			w.WriteHeader(502)
			return
		}
		defer upstream.Close()
		client, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer client.Close()
		if _, err := io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			t.Error(err)
			return
		}
		done := make(chan struct{})
		go func() { io.Copy(upstream, client); upstream.Close(); close(done) }()
		io.Copy(client, upstream)
		client.Close()
		<-done
	}))
	defer proxy.Close()
	m := probeManager(t, proxy.Listener.Addr().String(), "http", true)
	result, err := m.probeURL(context.Background(), "test", ProbeOptions{TimeoutSeconds: 3}, origin.URL)
	if err != nil || result.OK || result.Error == "" || connects.Load() != 1 || originRequests.Load() != 0 {
		t.Fatalf("TLS must reject untrusted certificate: result=%+v err=%v CONNECT=%d requests=%d", result, err, connects.Load(), originRequests.Load())
	}
}

func TestProbeBodyReadLimit(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		io.WriteString(w, strings.Repeat("x", 64<<10))
		w.(http.Flusher).Flush()
		// 超过读取上限后客户端必须结束，不等响应体 EOF。
		<-r.Context().Done()
	}))
	defer proxy.Close()
	m := probeManager(t, proxy.Listener.Addr().String(), "http", false)
	result, err := m.probeURL(context.Background(), "test", ProbeOptions{TimeoutSeconds: 2}, "http://target.invalid")
	if err != nil || !result.OK {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

// 最小回环 SOCKS5 服务验证标准库的用户密码握手和代理侧域名解析。
func TestProbeSOCKSAuthentication(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- func() error {
			conn, err := listener.Accept()
			if err != nil {
				return err
			}
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(3 * time.Second))
			reader := bufio.NewReader(conn)
			read := func(n int) ([]byte, error) { b := make([]byte, n); _, err := io.ReadFull(reader, b); return b, err }
			greeting, err := read(2)
			if err != nil {
				return err
			}
			if greeting[0] != 5 {
				return fmt.Errorf("version: %v", greeting)
			}
			if _, err := read(int(greeting[1])); err != nil {
				return err
			}
			if _, err := conn.Write([]byte{5, 2}); err != nil {
				return err
			}
			auth, err := read(2)
			if err != nil {
				return err
			}
			username, err := read(int(auth[1]))
			if err != nil {
				return err
			}
			length, err := reader.ReadByte()
			if err != nil {
				return err
			}
			password, err := read(int(length))
			if err != nil {
				return err
			}
			if auth[0] != 1 || string(username) != "probe-user" || string(password) != "p@ss:/?#word" {
				return fmt.Errorf("incorrect SOCKS credentials")
			}
			if _, err := conn.Write([]byte{1, 0}); err != nil {
				return err
			}
			connect, err := read(5)
			if err != nil {
				return err
			}
			if connect[0] != 5 || connect[1] != 1 || connect[3] != 3 {
				return fmt.Errorf("CONNECT must use domain: %v", connect)
			}
			host, err := read(int(connect[4]))
			if err != nil {
				return err
			}
			port, err := read(2)
			if err != nil {
				return err
			}
			if string(host) != "target.invalid" || binary.BigEndian.Uint16(port) != 80 {
				return fmt.Errorf("wrong SOCKS target")
			}
			if _, err := conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 80}); err != nil {
				return err
			}
			request, err := http.ReadRequest(reader)
			if err != nil {
				return err
			}
			request.Body.Close()
			if request.Header.Get("Proxy-Authorization") != "" || request.Header.Get("Authorization") != "" {
				return fmt.Errorf("credentials leaked to SOCKS origin request")
			}
			_, err = io.WriteString(conn, "HTTP/1.1 204 No Content\r\nConnection: close\r\n\r\n")
			return err
		}()
	}()
	m := probeManager(t, listener.Addr().String(), "mixed", true)
	result, probeErr := m.probeURL(context.Background(), "test", ProbeOptions{TimeoutSeconds: 3, ProxyProtocol: "socks5"}, "http://target.invalid")
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
	if probeErr != nil || !result.OK || result.StatusCode != 204 {
		t.Fatalf("result=%+v err=%v", result, probeErr)
	}
}

func TestProbeProtocolValidation(t *testing.T) {
	m := probeManager(t, "127.0.0.1:12345", "http", false)
	for _, protocol := range []string{"socks5", "direct", "https"} {
		if _, err := m.probeURL(context.Background(), "test", ProbeOptions{ProxyProtocol: protocol}, "https://target.invalid"); err == nil {
			t.Fatal("accepted invalid probe protocol", protocol)
		}
	}
}
