package engine

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRenamePersistenceAndSaveFailure(t *testing.T) {
	m := testManager(t, "absent")
	initial := Record{ID: nodeIDPrefix + "0123456789abcdef", Name: "original", Link: localPortLink, Protocol: "vless", Port: 12345}
	m.records = []Record{initial}
	renamed, err := m.Rename(initial.ID, "  🇩🇪 自定义名称  ")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Name != "🇩🇪 自定义名称" || renamed.Running || renamed.Enabled {
		t.Fatal("rename changed state")
	}
	expected := initial
	expected.Name = renamed.Name
	if !reflect.DeepEqual(expected, m.records[0]) {
		t.Fatal("rename changed connection/identity")
	}
	reopened, err := Open(m.dir, "absent")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if !reflect.DeepEqual(expected, reopened.records[0]) {
		t.Fatal("name not persisted")
	}
	original, _ := os.ReadFile(filepath.Join(m.dir, "nodes.json"))
	for _, bad := range []string{"", "  ", "line\nfeed", "\x00", strings.Repeat("中", 86), string([]byte{255})} {
		if _, err := m.Rename(initial.ID, bad); err == nil {
			t.Fatal("invalid name accepted")
		}
	}
	if _, err := m.Rename("missing", "name"); err == nil {
		t.Fatal("unknown node accepted")
	}
	stored, _ := os.ReadFile(filepath.Join(m.dir, "nodes.json"))
	if !bytes.Equal(original, stored) {
		t.Fatal("validation modified saved state")
	}
	m.dir = t.TempDir()
	if err := os.Mkdir(filepath.Join(m.dir, "nodes.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Rename(initial.ID, "failed"); err == nil {
		t.Fatal("save failure accepted")
	}
	if !reflect.DeepEqual(expected, m.records[0]) {
		t.Fatal("save failure changed in-memory record")
	}
}

func TestRenameKeepsRunningProcessAndEditedConnection(t *testing.T) {
	core := os.Getenv("XRAY_TEST_BINARY")
	if core == "" {
		t.Skip("设置 XRAY_TEST_BINARY 后执行真实内核测试")
	}
	m := testManager(t, core)
	first, err := m.AddWithSettings(localPortLink, Settings{})
	if err != nil {
		t.Fatal(err)
	}
	running := m.processes[first.ID]
	before := m.records[0]
	renamed, err := m.Rename(first.ID, "independent name")
	if err != nil {
		t.Fatal(err)
	}
	if m.processes[first.ID] != running || !alive(running) || renamed.ID != first.ID || renamed.Addr != first.Addr {
		t.Fatal("rename restarted process/changed identity")
	}
	expected := before
	expected.Name = renamed.Name
	if !reflect.DeepEqual(expected, m.records[0]) {
		t.Fatal("rename changed link/settings")
	}
	exported, err := m.Export(first.ID)
	if err != nil || exported.Name != renamed.Name || exported.ID != first.ID {
		t.Fatal("export name mismatch", err)
	}
	duplicate, err := m.AddWithSettings(localPortLink, Settings{})
	if err != nil || duplicate.ID != first.ID || duplicate.Name != renamed.Name {
		t.Fatal("reimport lost independent name", err)
	}
	edited, err := m.SetConnection(first.ID, localPortLink+"-edited")
	if err != nil || edited.Name != renamed.Name {
		t.Fatal("connection edit overwrote independent name", err)
	}
	if _, err := m.SetEnabled(first.ID, false); err != nil {
		t.Fatal(err)
	}
	stopped, err := m.Rename(first.ID, "stopped name")
	if err != nil || stopped.Running || stopped.Enabled {
		t.Fatal("rename started stopped node", err)
	}
}
