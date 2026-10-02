package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const localPortLink = "vless://" + testID + "@127.0.0.1:12345?security=none&type=tcp#ports"

func testFreePort(t *testing.T, reserved ...int) int {
	t.Helper()
	port, err := availablePort(reserved...)
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func testManager(t *testing.T, core string) *Manager {
	t.Helper()
	m, err := Open(t.TempDir(), core)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return m
}

func TestPortValidationAndMissingNode(t *testing.T) {
	m := testManager(t, "absent")
	for _, port := range []int{-1, 65536} {
		if _, err := m.AddWithSettings(localPortLink, Settings{Port: port}); err == nil {
			t.Fatalf("accepted add port %d", port)
		}
	}
	for _, port := range []int{-1, 0, 65536} {
		if _, err := testSetPort(m, "missing", port); err == nil || !strings.Contains(err.Error(), "1..65535") {
			t.Fatalf("invalid set port %d: %v", port, err)
		}
	}
	if _, err := testSetPort(m, "missing", 12345); err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("missing node: %v", err)
	}
	m.Close()
	if _, err := m.AddWithSettings(localPortLink, Settings{Port: 0}); err == nil {
		t.Fatal("closed manager accepted add")
	}
	if _, err := testSetPort(m, "missing", 12345); err == nil {
		t.Fatal("closed manager accepted edit")
	}
}

func TestPortConflictsAndAutomaticAllocation(t *testing.T) {
	for _, network := range []string{"tcp", "udp"} {
		t.Run(network, func(t *testing.T) {
			var port int
			if network == "tcp" {
				l, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				defer l.Close()
				port = l.Addr().(*net.TCPAddr).Port
			} else {
				l, err := net.ListenPacket("udp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				defer l.Close()
				port = l.LocalAddr().(*net.UDPAddr).Port
			}
			m := testManager(t, "absent")
			if _, err := m.AddWithSettings(localPortLink, Settings{Port: port}); err == nil || !strings.Contains(err.Error(), strings.ToUpper(network)) {
				t.Fatalf("occupied import: %v", err)
			}
			m.records = []Record{{ID: "xray-manager-0000000000000001", Port: testFreePort(t, port)}}
			before := m.records[0]
			if _, err := testSetPort(m, before.ID, port); err == nil || !strings.Contains(err.Error(), strings.ToUpper(network)) {
				t.Fatalf("occupied edit: %v", err)
			}
			if !reflect.DeepEqual(before, m.records[0]) {
				t.Fatal("failed edit mutated record")
			}
			automatic, err := availablePortFrom(port, nil)
			if err != nil || automatic == port {
				t.Fatalf("automatic allocation picked occupied port: %d %v", automatic, err)
			}
		})
	}
	reserved := testFreePort(t)
	automatic, err := availablePortFrom(reserved, []int{reserved})
	if err != nil || automatic == reserved {
		t.Fatalf("automatic allocation picked reservation: %d %v", automatic, err)
	}
}

func TestDisabledPortReservationAndPersistence(t *testing.T) {
	m := testManager(t, "absent")
	oldPort := testFreePort(t)
	reserved := testFreePort(t, oldPort)
	m.records = []Record{
		{ID: "xray-manager-0000000000000001", Name: "first", Protocol: "vless", Link: localPortLink, Port: oldPort},
		{ID: "xray-manager-0000000000000002", Name: "second", Protocol: "vless", Link: localPortLink + "2", Port: reserved},
	}
	if err := m.save(m.records); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AddWithSettings(localPortLink+"new", Settings{Port: reserved}); err == nil || !strings.Contains(err.Error(), "预留") {
		t.Fatalf("disabled reservation accepted: %v", err)
	}
	id := m.records[0].ID
	if _, err := testSetPort(m, id, reserved); err == nil || !strings.Contains(err.Error(), "预留") {
		t.Fatalf("reserved edit accepted: %v", err)
	}
	newPort := testFreePort(t, oldPort, reserved)
	view, err := testSetPort(m, id, newPort)
	if err != nil {
		t.Fatal(err)
	}
	if view.ID != id || view.Running || view.Enabled || view.Addr != fmt.Sprintf("127.0.0.1:%d", newPort) || !reflect.DeepEqual(view.PreviousAddrs, []string{fmt.Sprintf("127.0.0.1:%d", oldPort)}) {
		t.Fatalf("bad disabled edit: %+v", view)
	}
	if _, err := testSetPort(m, id, newPort); err != nil {
		t.Fatal(err)
	}
	if len(m.records[0].PreviousPorts) != 1 {
		t.Fatal("no-op duplicated history")
	}
	// 回到历史端口后再次修改，不重复追加同一历史端口。
	if _, err := testSetPort(m, id, oldPort); err != nil {
		t.Fatal(err)
	}
	if _, err := testSetPort(m, id, newPort); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(m.records[0].PreviousPorts, []int{oldPort, newPort}) {
		t.Fatal("history is not deduplicated", m.records[0])
	}
	m.Close()
	restored, err := Open(m.dir, "absent")
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if !reflect.DeepEqual(restored.records, m.records) || !reflect.DeepEqual(restored.List(), m.List()) {
		t.Fatal("persisted edit differs")
	}
	data, err := os.ReadFile(filepath.Join(m.dir, "nodes.json"))
	if err != nil || !bytes.Contains(data, []byte("previous_ports")) {
		t.Fatal("missing persisted history", err)
	}
	public, err := json.Marshal(restored.List())
	if err != nil || !bytes.Contains(public, []byte("previous_addrs")) || bytes.Contains(public, []byte(localPortLink)) {
		t.Fatal("bad public history", err)
	}
}

func TestSetPortDisabledSaveFailureIsUnchanged(t *testing.T) {
	m := testManager(t, "absent")
	oldPort := testFreePort(t)
	m.records = []Record{{ID: "xray-manager-0000000000000001", Link: localPortLink, Port: oldPort, PreviousPorts: []int{11111}}}
	before := m.view(m.records[0])
	m.failures[m.records[0].ID] = "original failure"
	before = m.view(m.records[0])
	m.dir = filepath.Join(m.dir, "missing")
	if _, err := testSetPort(m, before.ID, testFreePort(t, oldPort)); err == nil {
		t.Fatal("save failure accepted")
	}
	if !reflect.DeepEqual(before, m.List()[0]) || len(m.records[0].PreviousPorts) != 1 {
		t.Fatal("failure changed disabled record or error")
	}
}

func TestXrayCustomPortAndTransactionalChange(t *testing.T) {
	core := os.Getenv("XRAY_TEST_BINARY")
	if core == "" {
		t.Skip("设置 XRAY_TEST_BINARY 后执行真实内核测试")
	}
	m := testManager(t, core)
	first := testFreePort(t)
	view, err := m.AddWithSettings(localPortLink, Settings{Port: first})
	if err != nil {
		t.Fatal(err)
	}
	if view.Addr != fmt.Sprintf("127.0.0.1:%d", first) || !view.Running {
		t.Fatalf("custom port not running: %+v", view)
	}
	for _, port := range []int{0, first} {
		duplicate, err := m.AddWithSettings(localPortLink, Settings{Port: port})
		if err != nil || duplicate.ID != view.ID || len(m.List()) != 1 {
			t.Fatal("compatible duplicate rejected", err)
		}
	}
	target := testFreePort(t, first)
	if _, err := m.AddWithSettings(localPortLink, Settings{Port: target}); err == nil || !strings.Contains(err.Error(), "修改端口") {
		t.Fatalf("contradictory duplicate accepted: %v", err)
	}
	oldProcess := m.processes[view.ID]
	beforeData, err := os.ReadFile(filepath.Join(m.dir, "nodes.json"))
	if err != nil {
		t.Fatal(err)
	}
	// 核心启动失败时，原进程及原持久化文件保留。
	m.core = filepath.Join(m.dir, "absent")
	if _, err := testSetPort(m, view.ID, target); err == nil {
		t.Fatal("missing core accepted")
	}
	m.core = core
	assertOld := func() {
		t.Helper()
		if m.processes[view.ID] != oldProcess || !alive(oldProcess) || !reflect.DeepEqual(view, m.List()[0]) {
			t.Fatal("failure changed old view or process")
		}
		if err := socksReady(m.records[0]); err != nil {
			t.Fatal("old SOCKS unavailable", err)
		}
		data, err := os.ReadFile(filepath.Join(m.dir, "nodes.json"))
		if err != nil || !bytes.Equal(data, beforeData) {
			t.Fatal("failed transaction changed disk", err)
		}
		if err := checkAvailablePort(target); err != nil {
			t.Fatal("candidate port leaked", err)
		}
	}
	assertOld()
	// 保存目标为目录，强制原子替换失败，候选进程必须清理。
	originalDir := m.dir
	failingDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(failingDir, "nodes.json"), 0700); err != nil {
		t.Fatal(err)
	}
	m.dir = failingDir
	if _, err := testSetPort(m, view.ID, target); err == nil {
		t.Fatal("save failure accepted")
	}
	m.dir = originalDir
	assertOld()
	updated, err := testSetPort(m, view.ID, target)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != view.ID || !updated.Running || !updated.Enabled || updated.Addr != fmt.Sprintf("127.0.0.1:%d", target) || !reflect.DeepEqual(updated.PreviousAddrs, []string{view.Addr}) {
		t.Fatalf("bad port change: %+v", updated)
	}
	if alive(oldProcess) || m.processes[view.ID] == oldProcess {
		t.Fatal("old process not replaced")
	}
	if err := checkAvailablePort(first); err != nil {
		t.Fatal("old port not released", err)
	}
	exported, err := m.Export(view.ID)
	if err != nil || exported.ID != view.ID || exported.Addr != updated.Addr || exported.Username != "" || exported.Password != "" {
		t.Fatal("export changed contract", err)
	}
	if _, err := m.SetEnabled(view.ID, false); err != nil {
		t.Fatal(err)
	}
	automatic, err := m.AddWithSettings(localPortLink+"automatic", Settings{})
	if err != nil || !automatic.Running || automatic.Addr == updated.Addr {
		t.Fatal("automatic allocation failed or used disabled reservation", err)
	}
	if _, err := m.SetEnabled(view.ID, true); err != nil {
		t.Fatal(err)
	}
	m.Close()
	restored, err := Open(m.dir, core)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	got := restored.List()[0]
	if !reflect.DeepEqual(got, updated) {
		t.Fatalf("restart lost port/history/state: %+v", got)
	}
}

// 通过公开连接设置接口执行只修改端口的测试操作。
func testSetPort(m *Manager, id string, port int) (View, error) {
	for _, v := range m.List() {
		if v.ID == id {
			return m.SetSettings(id, Settings{Port: port, AllowLAN: v.AllowLAN, AuthEnabled: v.AuthEnabled, Username: v.Username})
		}
	}
	return m.SetSettings(id, Settings{Port: port})
}
