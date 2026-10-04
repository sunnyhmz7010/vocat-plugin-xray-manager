package node

import (
	"net/url"
	"reflect"
	"testing"
)

func TestKCPShareMask(t *testing.T) {
	headers := map[string]string{"none": "", "srtp": "header-srtp", "utp": "header-utp", "wechat-video": "header-wechat", "dtls": "header-dtls", "wireguard": "header-wireguard"}
	for header, mask := range headers {
		for _, seed := range []string{"absent", "", "test-kcp-seed"} {
			t.Run(header+"/"+seed, func(t *testing.T) {
				q := url.Values{"type": {"kcp"}, "headerType": {header}}
				if seed != "absent" {
					q.Set("seed", seed)
				}
				parsed, err := Parse("vless://" + testUUID + "@example.com:443?" + q.Encode())
				if err != nil {
					t.Fatal(err)
				}
				stream := parsed.Outbound["streamSettings"].(map[string]any)
				want := []any{}
				if mask != "" {
					want = append(want, map[string]any{"type": mask})
				}
				if seed != "absent" {
					want = append(want, map[string]any{"type": "mkcp-aes128gcm", "settings": map[string]any{"password": seed}})
				} else {
					want = append(want, map[string]any{"type": "mkcp-original"})
				}
				if !reflect.DeepEqual(stream["finalmask"], map[string]any{"udp": want}) {
					t.Fatal("mask order/type differs")
				}
				kcp := stream["kcpSettings"].(map[string]any)
				if _, ok := kcp["seed"]; ok {
					t.Fatal("removed seed field emitted")
				}
				if _, ok := kcp["header"]; ok {
					t.Fatal("removed header field emitted")
				}
				checkXray(t, parsed)
			})
		}
	}
	parsed, err := Parse("vless://" + testUUID + "@example.com:443?type=kcp")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := parsed.Outbound["streamSettings"].(map[string]any)["finalmask"]; ok {
		t.Fatal("native KCP mode changed")
	}
	for _, q := range []string{"type=kcp&headerType=http", "type=kcp&headerType=unknown", "type=ws&seed=bad", "type=grpc&headerType=utp"} {
		if _, err := Parse("vless://" + testUUID + "@example.com:443?" + q); err == nil {
			t.Fatal("accepted invalid KCP combination")
		}
	}
}
