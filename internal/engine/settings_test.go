package engine

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func password(s string) *string { return &s }

func TestSettingsValidation(t *testing.T) {
	initial := Record{Port: 12345}
	for _, s := range []Settings{
		{AuthEnabled: true, Username: "user"},
		{AuthEnabled: true, Username: "user", Password: password("")},
		{AuthEnabled: true, Username: "", Password: password("secret")},
		{AuthEnabled: true, Username: strings.Repeat("中", 86), Password: password("secret")},
		{AuthEnabled: true, Username: "user", Password: password(strings.Repeat("中", 86))},
		{AuthEnabled: true, Username: string([]byte{255}), Password: password("secret")},
	} {
		if _, err := applySettings(initial, s); err == nil {
			t.Fatalf("invalid settings accepted: %+v", s)
		}
	}
	r, err := applySettings(initial, Settings{AuthEnabled: true, Username: strings.Repeat("中", 85), Password: password(strings.Repeat("a", 255))})
	if err != nil {
		t.Fatal(err)
	}
	kept, err := applySettings(r, Settings{AuthEnabled: true, Username: "new-user"})
	if err != nil || kept.Password != r.Password || !reflect.DeepEqual(kept.PreviousUsernames, []string{r.Username}) {
		t.Fatal("password retention/history failed", err)
	}
	cleared, err := applySettings(kept, Settings{})
	if err != nil || cleared.Username != "" || cleared.Password != "" || len(cleared.PreviousUsernames) != 2 {
		t.Fatal("disable did not clear credentials/history", err)
	}
	if _, err := applySettings(cleared, Settings{AuthEnabled: true, Username: "new-user"}); err == nil {
		t.Fatal("reenable reused cleared password")
	}
}

func TestLANConfigOnly(t *testing.T) {
	// LAN 只断言配置，不在 Windows 或其他平台绑定全部接口。
	for _, auth := range []bool{false, true} {
		r := Record{Link: localPortLink, Port: 12345, AllowLAN: true, AuthEnabled: auth}
		if auth {
			r.Username, r.Password = "用户", "secret"
		}
		data, err := Config(r)
		if err != nil {
			t.Fatal(err)
		}
		var cfg struct {
			Inbounds []struct {
				Listen   string
				Settings map[string]any
			}
		}
		if err := json.Unmarshal(data, &cfg); err != nil {
			t.Fatal(err)
		}
		inbound := cfg.Inbounds[0]
		if inbound.Listen != "0.0.0.0" || inbound.Settings["udp"] != true {
			t.Fatal("LAN listen/UDP config invalid")
		}
		if _, ok := inbound.Settings["ip"]; ok {
			t.Fatal("LAN must use TCP connection local address in UDP reply")
		}
		if auth {
			if inbound.Settings["auth"] != "password" || len(inbound.Settings["accounts"].([]any)) != 1 {
				t.Fatal("missing authentication")
			}
		} else if inbound.Settings["auth"] != "noauth" || inbound.Settings["accounts"] != nil {
			t.Fatal("noauth leaked accounts")
		}
	}
	if listenHost(true) != "0.0.0.0" || listenHost(false) != "127.0.0.1" {
		t.Fatal("binding host mismatch")
	}
}

func TestXrayAuthenticationSettingsAndRollback(t *testing.T) {
	core := os.Getenv("XRAY_TEST_BINARY")
	if core == "" {
		t.Skip("设置 XRAY_TEST_BINARY 后执行真实内核测试")
	}
	m := testManager(t, core)
	s := Settings{Port: testFreePort(t), AuthEnabled: true, Username: "用户", Password: password("private-secret")}
	v, err := m.AddWithSettings(localPortLink, s)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Running || !v.AuthEnabled || !v.PasswordSet || v.Username != s.Username {
		t.Fatalf("bad view: %+v", v)
	}
	public, _ := json.Marshal(m.List())
	if bytes.Contains(public, []byte(*s.Password)) || bytes.Contains(public, []byte(`"password":`)) {
		t.Fatal("password leaked")
	}
	if err := socksReady(m.records[0]); err != nil {
		t.Fatal(err)
	}
	wrong := m.records[0]
	wrong.Password = "wrong"
	if err := socksReady(wrong); err == nil {
		t.Fatal("wrong password accepted")
	}
	wrong.AuthEnabled = false
	if err := socksReady(wrong); err == nil {
		t.Fatal("noauth accepted")
	}
	e, err := m.Export(v.ID)
	if err != nil || e.Username != s.Username || e.Password != *s.Password || e.Addr != v.Addr {
		t.Fatal("bad authenticated export", err)
	}
	if _, err := m.AddWithSettings(localPortLink, Settings{Port: s.Port}); err == nil {
		t.Fatal("duplicate reset accepted")
	}
	keep := s
	keep.Password = nil
	if _, err := m.AddWithSettings(localPortLink, keep); err != nil {
		t.Fatal("matching duplicate", err)
	}
	mismatch := keep
	mismatch.AllowLAN = true
	if _, err := m.AddWithSettings(localPortLink, mismatch); err == nil {
		t.Fatal("duplicate LAN reset accepted")
	}
	// 新端口仍保留认证。
	newPort := testFreePort(t, s.Port)
	v, err = testSetPort(m, v.ID, newPort)
	if err != nil || !v.AuthEnabled || !v.PasswordSet {
		t.Fatal("port update erased authentication", err)
	}
	keep.Port = newPort
	before := m.records[0]
	originalDir := m.dir
	failedDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(failedDir, "nodes.json"), 0700); err != nil {
		t.Fatal(err)
	}
	m.dir = failedDir
	changed := keep
	changed.Username = "updated-user"
	if _, err := m.SetSettings(v.ID, changed); err == nil {
		t.Fatal("save failure accepted")
	}
	m.dir = originalDir
	if !reflect.DeepEqual(m.records[0], before) || !m.List()[0].Running {
		t.Fatal("same-port rollback lost record/process")
	}
	if err := socksReady(before); err != nil {
		t.Fatal("restored credentials unavailable", err)
	}
	v, err = m.SetSettings(v.ID, changed)
	if err != nil || v.Username != "updated-user" || !reflect.DeepEqual(v.PreviousUsernames, []string{s.Username}) {
		t.Fatal("same-port update failed", err)
	}
	if err := socksReady(m.records[0]); err != nil {
		t.Fatal(err)
	}
	m.Close()
	restored, err := Open(originalDir, core)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if !reflect.DeepEqual(restored.List()[0], v) {
		t.Fatal("restart lost authentication/history")
	}
	v, err = restored.SetSettings(v.ID, Settings{Port: newPort})
	if err != nil || v.AuthEnabled || v.PasswordSet || v.Username != "" || len(v.PreviousUsernames) != 2 {
		t.Fatal("disable failed", err)
	}
	if restored.records[0].Password != "" || restored.records[0].Username != "" {
		t.Fatal("stored credentials not cleared")
	}
	if err := socksReady(restored.records[0]); err != nil {
		t.Fatal(err)
	}
	e, err = restored.Export(v.ID)
	if err != nil || e.Username != "" || e.Password != "" {
		t.Fatal("noauth export leaked credentials")
	}
}

func TestXraySamePortRestoreFailureReported(t *testing.T) {
	core := os.Getenv("XRAY_TEST_BINARY")
	if core == "" {
		t.Skip("设置 XRAY_TEST_BINARY 后执行真实内核测试")
	}
	m := testManager(t, core)
	v, err := m.AddWithSettings(localPortLink, Settings{})
	if err != nil {
		t.Fatal(err)
	}
	before := m.records[0]
	m.core = filepath.Join(t.TempDir(), "missing")
	_, err = m.SetSettings(v.ID, Settings{Port: before.Port, AuthEnabled: true, Username: "user", Password: password("secret")})
	if err == nil || !strings.Contains(err.Error(), "恢复旧进程失败") {
		t.Fatal("restore failure hidden", err)
	}
	if !reflect.DeepEqual(before, m.records[0]) || m.List()[0].Running || !strings.Contains(m.List()[0].Error, "恢复旧进程失败") {
		t.Fatal("rollback falsely reported running/success")
	}
}

func TestDisabledSettingsPersistence(t *testing.T) {
	m := testManager(t, "absent")
	r := Record{ID: "xray-manager-0000000000000001", Link: localPortLink, Port: testFreePort(t)}
	m.records = []Record{r}
	v, err := m.SetSettings(r.ID, Settings{Port: r.Port, AuthEnabled: true, Username: "user", Password: password("secret")})
	if err != nil || v.Enabled || v.Running || !v.AuthEnabled {
		t.Fatal("disabled edit changed state", err)
	}
	m.Close()
	restored, err := Open(m.dir, "absent")
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if !reflect.DeepEqual(restored.List()[0], v) {
		t.Fatal("disabled settings lost on restart")
	}
}

func TestSOCKSReadyRejectsMalformedRFC1929(t *testing.T) {
	for _, reply := range [][]byte{{5, 0}, {1, 1}, {1}} {
		t.Run(string([]byte{'0' + reply[0], '0' + byte(len(reply))}), func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			done := make(chan struct{})
			go func() {
				defer close(done)
				c, err := listener.Accept()
				if err != nil {
					return
				}
				defer c.Close()
				c.SetDeadline(time.Now().Add(time.Second))
				greeting := make([]byte, 3)
				if _, err = io.ReadFull(c, greeting); err != nil {
					return
				}
				if !bytes.Equal(greeting, []byte{5, 1, 2}) {
					return
				}
				c.Write([]byte{5, 2})
				credentials := make([]byte, 3+len("user")+len("secret"))
				if _, err = io.ReadFull(c, credentials); err != nil {
					return
				}
				c.Write(reply)
			}()
			if err := socksReady(Record{Port: listener.Addr().(*net.TCPAddr).Port, AuthEnabled: true, Username: "user", Password: "secret"}); err == nil {
				t.Fatal("malformed auth reply accepted")
			}
			<-done
		})
	}
}
