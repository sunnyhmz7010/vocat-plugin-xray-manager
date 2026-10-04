package node

import (
	"encoding/json"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestDownloadShareLinkMapping(t *testing.T) {
	raw := `{"downloadSettings":{"address":"download.example.com","port":443,"network":"xhttp","security":"tls","tlsSettings":{"serverName":"download.example.com","alpn":["h2"],"fingerprint":"chrome"},"xhttpSettings":{"path":"/download","host":"cdn.example.com","extra":{"noSSEHeader":true}}},"noGRPCHeader":true}`
	var want map[string]any
	if err := json.Unmarshal([]byte(raw), &want); err != nil {
		t.Fatal(err)
	}
	for _, network := range []string{"xhttp", "splithttp"} {
		q := url.Values{"type": {network}, "path": {"/upload"}, "mode": {"packet-up"}, "extra": {raw}}
		n, err := Parse("vless://" + testUUID + "@upload.example.com:80?" + q.Encode())
		if err != nil {
			t.Fatal(err)
		}
		got := n.Outbound["streamSettings"].(map[string]any)["xhttpSettings"].(map[string]any)["extra"]
		if !reflect.DeepEqual(got, want) {
			t.Fatal("download settings or sibling options changed")
		}
		checkXray(t, n)
		q.Set("mode", "stream-one")
		if _, err := Parse("vless://" + testUUID + "@upload.example.com:80?" + q.Encode()); err == nil || !strings.Contains(err.Error(), "stream-one") {
			t.Fatal("accepted incompatible upload mode", err)
		}
	}
	duplicate := `{"downloadSettings":{"address":"download.example.com","port":80,"port":443,"network":"xhttp"}}`
	if _, err := Parse("vless://" + testUUID + "@upload.example.com:80?type=xhttp&extra=" + url.QueryEscape(duplicate)); err == nil {
		t.Fatal("duplicate nested field accepted")
	}
}
