package node

import (
	"reflect"
	"testing"
)

func TestVLESSPacketEncodingNone(t *testing.T) {
	// 仅使用合成凭据与保留域名，不保存用户提供的节点。
	base := "vless://" + testUUID + "@example.com:443?flow=xtls-rprx-vision&fp=chrome&pbk=AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE&security=reality&sni=example.com&type=tcp"
	want, err := Parse(base + "#test")
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(base + "&packetEncoding=none#test")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("none must retain the default Xray configuration")
	}
	for _, value := range []string{"", "xudp", "packet", "unknown"} {
		if _, err := Parse(base + "&packetEncoding=" + value); err == nil {
			t.Fatalf("accepted unsupported encoding %q", value)
		}
	}
	if _, err := Parse(base + "&packetEncoding=none&packetEncoding=none"); err == nil {
		t.Fatal("accepted duplicate encoding")
	}
}
