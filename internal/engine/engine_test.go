package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testID = "6ba7b810-9dad-41d1-80b4-00c04fd430c8"

func TestMissingCoreDoesNotPersist(t *testing.T) {
	dir := t.TempDir()
	m, err := Open(dir, filepath.Join(dir, "absent"))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	_, err = m.AddWithSettings("vless://"+testID+"@127.0.0.1:12345?security=none&type=tcp#test", Settings{})
	if err == nil {
		t.Fatal("expected missing core error")
	}
	if len(m.List()) != 0 {
		t.Fatal("failed import persisted")
	}
	if _, err := os.Stat(filepath.Join(dir, "nodes.json")); !os.IsNotExist(err) {
		t.Fatal("unexpected saved state", err)
	}
}
func TestCorruptStorePreserved(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "nodes.json")
	original := []byte("broken")
	if err := os.WriteFile(file, original, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir, "absent"); err == nil {
		t.Fatal("expected corruption error")
	}
	data, _ := os.ReadFile(file)
	if !bytes.Equal(data, original) {
		t.Fatal("corrupt store overwritten")
	}
}

func TestXrayLifecycleAndForwarding(t *testing.T) {
	core := os.Getenv("XRAY_TEST_BINARY")
	if core == "" {
		t.Skip("设置 XRAY_TEST_BINARY 后执行真实内核测试")
	}
	serverPort, err := availablePort()
	if err != nil {
		t.Fatal(err)
	}
	config := map[string]any{"log": map[string]any{"loglevel": "none"}, "inbounds": []any{map[string]any{"listen": "127.0.0.1", "port": serverPort, "protocol": "vless", "settings": map[string]any{"clients": []any{map[string]string{"id": testID}}, "decryption": "none"}}}, "outbounds": []any{map[string]any{"protocol": "freedom"}}}
	data, _ := json.Marshal(config)
	cmd := exec.Command(core, "run", "-config", "stdin:", "-format", "json")
	cmd.Stdin = bytes.NewReader(data)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", serverPort), 100*time.Millisecond)
		if err == nil {
			c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("test VLESS server not ready")
		}
		time.Sleep(30 * time.Millisecond)
	}
	dir := t.TempDir()
	m, err := Open(dir, core)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	link := fmt.Sprintf("vless://%s@127.0.0.1:%d?security=none&type=tcp#local", testID, serverPort)
	view, err := m.AddWithSettings(link, Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if !view.Running {
		t.Fatal("not running")
	}
	duplicate, err := m.AddWithSettings(link, Settings{})
	if err != nil || duplicate.ID != view.ID || len(m.List()) != 1 {
		t.Fatal("duplicate import")
	}
	export, err := m.Export(view.ID)
	if err != nil {
		t.Fatal(err)
	}
	public, _ := json.Marshal(m.List())
	if export.Username != "" || export.Password != "" {
		t.Fatal("local SOCKS credentials must be empty")
	}
	if strings.Contains(string(public), link) {
		t.Fatal("secret leaked")
	}
	tcpEcho, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer tcpEcho.Close()
	go func() {
		for {
			c, e := tcpEcho.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); c.SetDeadline(time.Now().Add(5 * time.Second)); _, _ = io.Copy(c, c) }()
		}
	}()
	control := socksConnect(t, export, 1, tcpEcho.Addr().(*net.TCPAddr).Port)
	message := []byte("vocat-through-vless")
	if _, err = control.Write(message); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, len(message))
	if _, err = io.ReadFull(control, reply); err != nil {
		t.Fatal(err)
	}
	control.Close()
	if !bytes.Equal(message, reply) {
		t.Fatal("TCP payload differs")
	}
	// UDP ASSOCIATE 与实际 UDP 回包，覆盖 VoWiFi 场景依赖的 UDP 转发。
	udpEcho, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer udpEcho.Close()
	go func() {
		buf := make([]byte, 2048)
		for {
			n, peer, e := udpEcho.ReadFrom(buf)
			if e != nil {
				return
			}
			_, _ = udpEcho.WriteTo(buf[:n], peer)
		}
	}()
	association := socksConnect(t, export, 3, 0)
	defer association.Close()
	u, err := net.Dial("udp", export.Addr)
	if err != nil {
		t.Fatal(err)
	}
	defer u.Close()
	u.SetDeadline(time.Now().Add(5 * time.Second))
	port := udpEcho.LocalAddr().(*net.UDPAddr).Port
	packet := append([]byte{0, 0, 0, 1, 127, 0, 0, 1, byte(port >> 8), byte(port)}, message...)
	if _, err = u.Write(packet); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 2048)
	n, err := u.Read(buffer)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(buffer[:n], message) {
		t.Fatal("UDP payload differs")
	}
	association.Close()
	u.Close()
	m.Close()
	m2, err := Open(dir, core)
	if err != nil {
		t.Fatal(err)
	}
	defer m2.Close()
	restored, err := m2.Export(view.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored != export {
		t.Fatal("restart changed mapping")
	}
	if _, err := m2.SetEnabled(view.ID, false); err != nil {
		t.Fatal(err)
	}
	if m2.List()[0].Running {
		t.Fatal("stop left process running")
	}
	listener, err := net.Listen("tcp", export.Addr)
	if err != nil {
		t.Fatal("port not released", err)
	}
	if _, err := m2.SetEnabled(view.ID, true); err == nil {
		t.Fatal("occupied port accepted")
	}
	listener.Close()
	if _, err := m2.SetEnabled(view.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := m2.Delete(view.ID); err != nil {
		t.Fatal(err)
	}
	if len(m2.List()) != 0 {
		t.Fatal("delete failed")
	}
}
func socksConnect(t *testing.T, e Export, command byte, port int) net.Conn {
	t.Helper()
	c, err := net.DialTimeout("tcp", e.Addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	c.SetDeadline(time.Now().Add(8 * time.Second))
	if _, err = c.Write([]byte{5, 1, 0}); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 2)
	if _, err = io.ReadFull(c, reply); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(reply, []byte{5, 0}) {
		t.Fatal("no-auth negotiation failed")
	}
	if _, err = c.Write([]byte{5, command, 0, 1, 127, 0, 0, 1, byte(port >> 8), byte(port)}); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, 10)
	if _, err = io.ReadFull(c, response); err != nil {
		t.Fatal(err)
	}
	if response[1] != 0 {
		t.Fatal("SOCKS request rejected", response)
	}
	return c
}
