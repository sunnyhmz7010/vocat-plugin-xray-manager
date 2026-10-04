package engine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestInboundSettingsAndConfig(t *testing.T) {
	off := false
	for _, mode := range []string{"", "mixed", "http"} {
		for _, udp := range []*bool{nil, &off} {
			r, err := applySettings(Record{Link: localPortLink}, Settings{InboundMode: mode, UDPEnabled: udp, AuthEnabled: true, Username: "user", Password: password("private-secret")})
			if err != nil {
				t.Fatal(err)
			}
			data, err := Config(r)
			if err != nil {
				t.Fatal(err)
			}
			var c struct {
				Inbounds []struct {
					Protocol string
					Settings map[string]any
				}
			}
			if err = json.Unmarshal(data, &c); err != nil {
				t.Fatal(err)
			}
			in := c.Inbounds[0]
			if mode == "http" {
				if in.Protocol != "http" || in.Settings["allowTransparent"] != false || in.Settings["udp"] != nil || in.Settings["auth"] != nil {
					t.Fatal("bad HTTP config")
				}
			} else if in.Protocol != "socks" || in.Settings["udp"] != (udp == nil) {
				t.Fatal("bad mixed UDP config")
			}
			if in.Settings["accounts"] == nil {
				t.Fatal("accounts lost")
			}
			v := (&Manager{}).view(r)
			if v.InboundMode != normalizedInboundMode(mode) || v.UDPEnabled != recordUDPEnabled(r) {
				t.Fatal("bad view")
			}
			public, _ := json.Marshal(v)
			if bytes.Contains(public, []byte(r.Password)) {
				t.Fatal("password leaked")
			}
		}
	}
	for _, mode := range []string{"socks", "pure-socks", "HTTP", "invalid"} {
		if _, err := applySettings(Record{}, Settings{InboundMode: mode}); err == nil {
			t.Fatal("invalid mode accepted")
		}
	}
	if !sameSettings(Record{}, Record{InboundMode: "mixed"}) || sameSettings(Record{}, Record{InboundMode: "http"}) || sameSettings(Record{}, Record{UDPDisabled: true}) {
		t.Fatal("settings equality")
	}
}

func TestInboundUDPBinding(t *testing.T) {
	port := testFreePort(t)
	u, err := net.ListenPacket("udp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	defer u.Close()
	m := testManager(t, "absent")
	for _, r := range []Record{{Port: port, InboundMode: "http"}, {Port: port, UDPDisabled: true}} {
		if err = m.checkRecordBinding(r); err != nil {
			t.Fatal(err)
		}
	}
	if err = m.checkBinding(port, "", false); err == nil {
		t.Fatal("legacy helper must check UDP")
	}
	if p, err := availablePortFromBindingUDP(port, nil, false, false); err != nil || p != port {
		t.Fatal("TCP allocation rejected UDP occupation", p, err)
	}
}

// 与现有生命周期测试相同的回环 VLESS->freedom fixture，所有目标均为回环。
func inboundVLESS(t *testing.T, core string) string {
	t.Helper()
	port := testFreePort(t)
	config := map[string]any{"log": map[string]any{"loglevel": "none"}, "inbounds": []any{map[string]any{"listen": "127.0.0.1", "port": port, "protocol": "vless", "settings": map[string]any{"clients": []any{map[string]string{"id": testID}}, "decryption": "none"}}}, "outbounds": []any{map[string]any{"protocol": "freedom"}}}
	data, _ := json.Marshal(config)
	cmd := exec.Command(core, "run", "-config", "stdin:", "-format", "json")
	cmd.Stdin = bytes.NewReader(data)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	deadline := time.Now().Add(5 * time.Second)
	for {
		c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
		if err == nil {
			c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("VLESS fixture not ready")
		}
		time.Sleep(30 * time.Millisecond)
	}
	return fmt.Sprintf("vless://%s@127.0.0.1:%d?security=none&type=tcp#inbound", testID, port)
}

func TestXrayInboundForwardingAndSwitch(t *testing.T) {
	core := os.Getenv("XRAY_TEST_BINARY")
	if core == "" {
		t.Skip("设置 XRAY_TEST_BINARY 后执行真实内核测试")
	}
	link := inboundVLESS(t, core)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "loopback-through-vless") }))
	defer origin.Close()
	echo, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		for {
			c, e := echo.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); c.SetDeadline(time.Now().Add(5 * time.Second)); io.Copy(c, c) }()
		}
	}()
	for _, mode := range []string{"http", "mixed"} {
		for _, auth := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/auth=%v", mode, auth), func(t *testing.T) {
				m := testManager(t, core)
				off := false
				s := Settings{InboundMode: mode, UDPEnabled: &off, AuthEnabled: auth}
				if auth {
					s.Username = "user"
					s.Password = password("private-secret")
				}
				// TCP-only modes must start even when another application owns the UDP port.
				s.Port = testFreePort(t)
				u, e := net.ListenPacket("udp", fmt.Sprintf("127.0.0.1:%d", s.Port))
				if e != nil {
					t.Fatal(e)
				}
				defer u.Close()
				v, e := m.AddWithSettings(link, s)
				if e != nil {
					t.Fatal(e)
				}
				if !v.Running || v.UDPEnabled {
					t.Fatal("bad running state")
				}
				if e = httpReady(m.records[0]); e != nil {
					t.Fatal(e)
				}
				proxyURL, _ := url.Parse("http://" + v.Addr)
				if auth {
					proxyURL.User = url.UserPassword(s.Username, *s.Password)
				}
				transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
				defer transport.CloseIdleConnections()
				client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
				resp, e := client.Get(origin.URL)
				if e != nil {
					t.Fatal(e)
				}
				body, e := io.ReadAll(resp.Body)
				resp.Body.Close()
				if e != nil || string(body) != "loopback-through-vless" {
					t.Fatal("HTTP did not forward", e, resp.Status)
				}
				probeProtocols := []string{"http"}
				if mode == "mixed" {
					probeProtocols = append(probeProtocols, "socks5")
				}
				for _, protocol := range probeProtocols {
					result, err := m.Probe(context.Background(), v.ID, ProbeOptions{URL: origin.URL, ProxyProtocol: protocol, TimeoutSeconds: 5})
					if err != nil || !result.OK || result.StatusCode != 200 {
						t.Fatalf("real Xray probe %s: %+v %v", protocol, result, err)
					}
				}
				for _, wrong := range []bool{false, true} {
					c, e := net.DialTimeout("tcp", v.Addr, time.Second)
					if e != nil {
						t.Fatal(e)
					}
					c.SetDeadline(time.Now().Add(5 * time.Second))
					authHeader := ""
					if auth {
						pass := *s.Password
						if wrong {
							pass = "wrong"
						}
						authHeader = "Proxy-Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(s.Username+":"+pass)) + "\r\n"
					} else if wrong {
						c.Close()
						continue
					}
					fmt.Fprintf(c, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n%s\r\n", echo.Addr(), echo.Addr(), authHeader)
					reader := bufio.NewReader(c)
					response, e := http.ReadResponse(reader, &http.Request{Method: "CONNECT"})
					if e != nil {
						c.Close()
						t.Fatal(e)
					}
					if wrong {
						if response.StatusCode != 407 {
							t.Fatal("wrong auth accepted")
						}
						c.Close()
						continue
					}
					if response.StatusCode != 200 {
						t.Fatal("CONNECT rejected", response.Status)
					}
					message := []byte("connect-through-vless")
					c.Write(message)
					reply := make([]byte, len(message))
					_, e = io.ReadFull(reader, reply)
					c.Close()
					if e != nil || !bytes.Equal(reply, message) {
						t.Fatal("CONNECT payload mismatch", e)
					}
				}
				if mode == "mixed" {
					if e = socksReady(m.records[0]); e != nil {
						t.Fatal(e)
					}
					if !auth {
						ex, e := m.Export(v.ID)
						if e != nil {
							t.Fatal(e)
						}
						c := socksConnect(t, ex, 1, echo.Addr().(*net.TCPAddr).Port)
						c.Write([]byte("socks"))
						b := make([]byte, 5)
						_, e = io.ReadFull(c, b)
						c.Close()
						if e != nil || string(b) != "socks" {
							t.Fatal("mixed SOCKS forwarding", e)
						}
					}
				} else if _, e = m.Export(v.ID); e == nil || !strings.Contains(e.Error(), "VoCat 上游只支持 SOCKS") {
					t.Fatal("HTTP export accepted", e)
				}
				before := m.records[0]
				s.Password = nil
				if mode == "http" {
					s.InboundMode = "mixed"
				} else {
					s.InboundMode = "http"
				}
				v, e = m.SetSettings(v.ID, s)
				if e != nil {
					t.Fatal(e)
				}
				if m.records[0].Password != before.Password || !reflect.DeepEqual(m.records[0].PreviousPorts, before.PreviousPorts) || !reflect.DeepEqual(m.records[0].PreviousUsernames, before.PreviousUsernames) {
					t.Fatal("switch lost credentials/history")
				}
				dir := m.dir
				m.Close()
				restored, e := Open(dir, core)
				if e != nil {
					t.Fatal(e)
				}
				defer restored.Close()
				if !reflect.DeepEqual(v, restored.List()[0]) {
					t.Fatal("restart differs")
				}
				if _, e = restored.SetEnabled(v.ID, false); e != nil {
					t.Fatal(e)
				}
				if e = restored.Delete(v.ID); e != nil {
					t.Fatal(e)
				}
			})
		}
	}
}

func TestXrayInboundRollbackAndTCPOnlyReadiness(t *testing.T) {
	core := os.Getenv("XRAY_TEST_BINARY")
	if core == "" {
		t.Skip("设置 XRAY_TEST_BINARY 后执行真实内核测试")
	}
	for _, samePort := range []bool{true, false} {
		t.Run(fmt.Sprintf("samePort=%v", samePort), func(t *testing.T) {
			m := testManager(t, core)
			// 默认UDP偏好开启的HTTP仍只占用TCP，且目标不可用不阻碍启动。
			port := testFreePort(t)
			u, err := net.ListenPacket("udp", fmt.Sprintf("127.0.0.1:%d", port))
			if err != nil {
				t.Fatal(err)
			}
			defer u.Close()
			s := Settings{Port: port, InboundMode: "http", AuthEnabled: true, Username: "user", Password: password("retained-secret")}
			v, err := m.AddWithSettings(localPortLink, s)
			if err != nil {
				t.Fatal(err)
			}
			if v.UDPEnabled || m.records[0].UDPDisabled {
				t.Fatal("HTTP effective UDP changed saved preference")
			}
			before := m.records[0]
			udpAttempt := s
			udpAttempt.InboundMode = "mixed"
			if _, err = m.SetSettings(v.ID, udpAttempt); err == nil || !strings.Contains(err.Error(), "UDP") {
				t.Fatal("mixed UDP accepted occupied port", err)
			}
			if !reflect.DeepEqual(before, m.records[0]) || httpReady(before) != nil {
				t.Fatal("UDP failure did not restore HTTP")
			}
			original := m.processes[v.ID]
			dir := m.dir
			failed := t.TempDir()
			if err = os.Mkdir(filepath.Join(failed, "nodes.json"), 0700); err != nil {
				t.Fatal(err)
			}
			m.dir = failed
			off := false
			s.InboundMode = "mixed"
			s.UDPEnabled = &off
			s.Password = nil
			if !samePort {
				s.Port = testFreePort(t, port)
			}
			if _, err = m.SetSettings(v.ID, s); err == nil {
				t.Fatal("save failure accepted")
			}
			m.dir = dir
			if !reflect.DeepEqual(before, m.records[0]) || !m.List()[0].Running {
				t.Fatal("rollback lost original settings/process")
			}
			if !samePort && m.processes[v.ID] != original {
				t.Fatal("candidate failure replaced original process")
			}
			if err = httpReady(before); err != nil {
				t.Fatal("original HTTP not restored", err)
			}
			if !samePort {
				if err = checkAvailableBinding(s.Port, false); err != nil {
					t.Fatal("candidate leaked listener", err)
				}
			}
			v, err = m.SetSettings(v.ID, s)
			if err != nil {
				t.Fatal(err)
			}
			if err = socksReady(m.records[0]); err != nil {
				t.Fatal(err)
			}
			if m.records[0].Password != before.Password || v.InboundMode != "mixed" {
				t.Fatal("successful switch changed credentials")
			}
		})
	}
}

func TestHTTPCompatibleUsername(t *testing.T) {
	if _, err := applySettings(Record{}, Settings{AuthEnabled: true, Username: "a:b", Password: password("secret")}); err == nil {
		t.Fatal("accepted colon in HTTP Basic username")
	}
}
