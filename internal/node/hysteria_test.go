package node

import (
	"reflect"
	"strings"
	"testing"
)

func TestHysteria2Mapping(t *testing.T) {
	for _, scheme := range []string{"hy2", "hysteria2"} {
		for _, credential := range []string{"user:pass%40word", "user%3Apass%40word"} {
			n, err := Parse(scheme + "://" + credential + "@[::1]/?sni=localhost&obfs=salamander&obfs-password=mask%26password&pinSHA256=" + strings.Repeat("AB", 32) + "#测试")
			if err != nil {
				t.Fatal(err)
			}
			if n.Name != "测试" || n.Protocol != "hysteria2" || n.Outbound["protocol"] != "hysteria" {
				t.Fatal("protocol/name mapping")
			}
			if !reflect.DeepEqual(n.Outbound["settings"], map[string]any{"version": 2, "address": "::1", "port": 443}) {
				t.Fatal("endpoint mapping")
			}
			s := n.Outbound["streamSettings"].(map[string]any)
			if s["network"] != "hysteria" || s["security"] != "tls" || !reflect.DeepEqual(s["hysteriaSettings"], map[string]any{"version": 2, "auth": "user:pass@word"}) {
				t.Fatal("transport/auth mapping")
			}
			tls := s["tlsSettings"].(map[string]any)
			if tls["allowInsecure"] != false || tls["serverName"] != "localhost" || tls["pinnedPeerCertSha256"] != strings.Repeat("ab", 32) || !reflect.DeepEqual(tls["alpn"], []string{"h3"}) {
				t.Fatal("TLS mapping")
			}
			mask := s["finalmask"].(map[string]any)["udp"].([]any)[0].(map[string]any)
			if mask["type"] != "salamander" || mask["settings"].(map[string]any)["password"] != "mask&password" {
				t.Fatal("obfuscation mapping")
			}
		}
	}
	for _, link := range []string{"hy2://example.com", "hy2://secret@example.com:8443?insecure=0", "hysteria2://secret@example.com?insecure=false"} {
		n, err := Parse(link)
		if err != nil {
			t.Fatal(err)
		}
		if n.Name != "hysteria2 节点" || n.Outbound["streamSettings"].(map[string]any)["finalmask"] != nil {
			t.Fatal("defaults")
		}
	}
}

func TestHysteria2RejectsInvalidLinks(t *testing.T) {
	for _, suffix := range []string{
		"?insecure=1", "?insecure=true", "?insecure=", "?insecure=maybe",
		"?insecure=1&pinSHA256=", "?insecure=true&pinSHA256=wrong",
		"?sni=a&sni=b", "?sni=", "?sni=bad%20host", "?sni=bad%0Ahost",
		"?obfs=gecko", "?obfs=salamander", "?obfs-password=secret", "?obfs=salamander&obfs-password=%00",
		"?pinSHA256=", "?pinSHA256=wrong", "?ech=secret", "?unknown=secret", "?sni=%ZZ", "?up=100",
		"/path", "#bad%0Aname",
	} {
		_, err := Parse("hy2://private-credential@example.com:443" + suffix)
		if err == nil {
			t.Fatalf("accepted %s", suffix)
		}
		if strings.Contains(err.Error(), "private-credential") {
			t.Fatal("credential leaked")
		}
	}
	for _, link := range []string{"hy2://", "hy2://example.com:", "hy2://example.com:0", "hy2://example.com:65536", "hy2://example.com:443,8443", "hy2://example.com:1000-2000", "hy2://::1", "hy2://bad%0Aauth@example.com"} {
		if _, err := Parse(link); err == nil {
			t.Fatalf("accepted %s", link)
		}
	}
}

func TestHysteria2InsecureWithPin(t *testing.T) {
	for _, insecure := range []string{"1", "true"} {
		// 使用虚构凭据与文档地址，保留实际分享链接的参数组合和编码形式。
		link := "hysteria2://test-password@192.0.2.1:443?insecure=" + insecure + "&sni=192.0.2.1&pinSHA256=" + strings.Repeat("ab", 32) + "#%F0%9F%87%AF%F0%9F%87%B5%20%E4%B8%9C%E4%BA%AC"
		n, err := Parse(link)
		if err != nil {
			t.Fatal(err)
		}
		tls := n.Outbound["streamSettings"].(map[string]any)["tlsSettings"].(map[string]any)
		if tls["allowInsecure"] != false || tls["pinnedPeerCertSha256"] != strings.Repeat("ab", 32) || tls["serverName"] != "192.0.2.1" || n.Name != "🇯🇵 东京" {
			t.Fatal("insecure + pin mapping failed")
		}
		withoutInsecure, err := Parse(strings.Replace(link, "insecure="+insecure+"&", "", 1))
		if err != nil || !reflect.DeepEqual(n.Outbound, withoutInsecure.Outbound) {
			t.Fatal("pinned verification semantics changed")
		}
	}
}
