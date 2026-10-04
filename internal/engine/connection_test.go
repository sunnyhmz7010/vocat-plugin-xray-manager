package engine

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestConnectionEditKeepsIdentityAndRollsBack(t *testing.T) {
	core := os.Getenv("XRAY_TEST_BINARY")
	if core == "" {
		t.Skip("设置 XRAY_TEST_BINARY 后执行")
	}
	m := testManager(t, core)
	first, err := m.AddWithSettings(localPortLink, Settings{})
	if err != nil {
		t.Fatal(err)
	}
	before := m.records[0]
	link := localPortLink + "-edited"
	updated, err := m.SetConnection(first.ID, link)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != first.ID || updated.Addr != first.Addr || !updated.Running {
		t.Fatal("edit changed local identity/state")
	}
	if stored, err := m.Connection(first.ID); err != nil || stored != link {
		t.Fatal("connection not saved", err)
	}
	duplicate, err := m.AddWithSettings(link, Settings{})
	if err != nil || duplicate.ID != first.ID {
		t.Fatal("reimport edited link lost identity", err)
	}
	if _, err := m.AddWithSettings(localPortLink, Settings{}); err == nil {
		t.Fatal("original link silently reused edited node")
	}
	before = m.records[0]
	dir := m.dir
	broken := t.TempDir()
	if err := os.Mkdir(filepath.Join(broken, "nodes.json"), 0700); err != nil {
		t.Fatal(err)
	}
	m.dir = broken
	if _, err := m.SetConnection(first.ID, localPortLink+"-failure"); err == nil {
		t.Fatal("save failure accepted")
	}
	m.dir = dir
	if !reflect.DeepEqual(before, m.records[0]) || !m.List()[0].Running {
		t.Fatal("failed edit lost original record/service")
	}
	if err := socksReady(before); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetConnection(first.ID, "invalid"); err == nil {
		t.Fatal("invalid link accepted")
	}
	if _, err := m.SetEnabled(first.ID, false); err != nil {
		t.Fatal(err)
	}
	if updated, err = m.SetConnection(first.ID, localPortLink+"-stopped"); err != nil || updated.Running || updated.Enabled {
		t.Fatal("editing stopped node started it", err)
	}
}
