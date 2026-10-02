package engine

import (
	"encoding/json"
	"testing"
)

func TestSOCKSLoopbackWithoutCredentials(t *testing.T) {
	data, err := Config(Record{Link: "vless://" + testID + "@example.com:443?security=none", Port: 12345})
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Inbounds []struct {
			Listen   string         `json:"listen"`
			Settings map[string]any `json:"settings"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Inbounds) != 1 || cfg.Inbounds[0].Listen != "127.0.0.1" {
		t.Fatal("must listen only on loopback")
	}
	settings := cfg.Inbounds[0].Settings
	if settings["auth"] != "noauth" || settings["ip"] != "127.0.0.1" || settings["udp"] != true {
		t.Fatal("unexpected SOCKS settings")
	}
	if _, ok := settings["accounts"]; ok {
		t.Fatal("noauth must not contain accounts")
	}
}
