package main

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"vocat-plugin-xray-manager/internal/engine"
)

func TestPrivateStorageOutsidePluginAssets(t *testing.T) {
	root := t.TempDir()
	plugin := filepath.Join(root, "xray-manager")
	got, err := privateDataDir(filepath.Join(plugin, "data"))
	if err != nil || got != filepath.Join(root, ".xray-manager-private") {
		t.Fatalf("private storage: %s %v", got, err)
	}
	if rel, _ := filepath.Rel(plugin, got); !strings.HasPrefix(rel, "..") {
		t.Fatal("credentials within static asset root")
	}
	for _, wrong := range []string{root, filepath.Join(root, "other", "data"), filepath.Join(plugin, "secrets")} {
		if _, err := privateDataDir(wrong); err == nil {
			t.Fatal("accepted unexpected host layout")
		}
	}
}

func TestHTTPBoundary(t *testing.T) {
	m, err := engine.Open(t.TempDir(), filepath.Join(t.TempDir(), "missing-core"))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	h := handler(m)
	for _, tc := range []struct {
		name, method, path, body, plugin, origin, mime string
		want                                           int
	}{
		{"rename-bypass", "PUT", "/nodes/unknown/name", `{"name":"new"}`, "", "", "application/json", 403},
		{"rename-extra", "PUT", "/nodes/unknown/name", `{"name":"new","link":"secret"}`, "xray-manager", "", "application/json", 400},
		{"rename-type", "PUT", "/nodes/unknown/name", `{"name":123}`, "xray-manager", "", "application/json", 400},
		{"rename-trailing", "PUT", "/nodes/unknown/name", `{"name":"new"}{}`, "xray-manager", "", "application/json", 400},
		{"port-type", "POST", "/nodes", `{"link":"x","port":"1080"}`, "xray-manager", "", "application/json", 400},
		{"settings-type", "PUT", "/nodes/unknown/settings", `{"port":1080,"allow_lan":"true"}`, "xray-manager", "", "application/json", 400},
		{"settings-mask", "PUT", "/nodes/unknown/settings", `{"port":1080,"auth_enabled":true,"username":"test","password":"********"}`, "xray-manager", "", "application/json", 400},
		{"settings-whitespace", "PUT", "/nodes/unknown/settings", `{"port":1080,"auth_enabled":true,"username":" test ","password":"test"}`, "xray-manager", "", "application/json", 400},
		{"settings-extra", "PUT", "/nodes/unknown/settings", `{"port":1080}{}`, "xray-manager", "", "application/json", 400},
		{"settings-unknown", "PUT", "/nodes/unknown/settings", `{"port":1080,"listen":"0.0.0.0"}`, "xray-manager", "", "application/json", 400},
		{"settings-bypass", "PUT", "/nodes/unknown/settings", `{"port":1080}`, "", "", "application/json", 403},
		{"health", "GET", "/healthz", "", "xray-manager", "", "", 200},
		{"bypass", "GET", "/nodes", "", "", "", "", 403},
		{"cross-origin", "GET", "/nodes", "", "xray-manager", "https://attacker.example", "", 403},
		{"same-origin", "GET", "/nodes", "", "xray-manager", "http://vocat.example", "", 200},
		{"form", "POST", "/nodes", "link=bad", "xray-manager", "", "application/x-www-form-urlencoded", 415},
		{"unknown-field", "POST", "/nodes", `{"link":"x","extra":true}`, "xray-manager", "", "application/json", 400},
		{"extra-json", "POST", "/nodes", `{"link":"x"}{}`, "xray-manager", "", "application/json", 400},
		{"invalid-link", "POST", "/nodes", `{"link":"https://invalid.example"}`, "xray-manager", "", "application/json", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "http://vocat.example"+tc.path, strings.NewReader(tc.body))
			r.Header.Set("X-VoCat-Plugin-ID", tc.plugin)
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Content-Type", tc.mime)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status %d want %d: %s", w.Code, tc.want, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("sensitive response cacheable")
			}
		})
	}
}
