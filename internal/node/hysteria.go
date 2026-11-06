package node

import (
	"net/url"
	"strings"
)

// parseHysteria2 映射到固定 Xray v26.3.27 的协议层、QUIC 传输及 finalmask。
func parseHysteria2(raw string) (Node, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" || (u.Path != "" && u.Path != "/") {
		return Node{}, fail("Hysteria 2 链接格式无效；端口仅支持单个整数，暂不支持端口跳跃")
	}
	if strings.Count(u.Host, ":") > 1 && !strings.HasPrefix(u.Host, "[") {
		return Node{}, fail("IPv6 地址必须使用方括号")
	}
	portText := u.Port()
	if portText == "" {
		if strings.HasSuffix(u.Host, ":") {
			return Node{}, fail("端口不能为空")
		}
		portText = "443"
	}
	host, port, err := endpoint(u.Hostname(), portText)
	if err != nil {
		return Node{}, err
	}
	auth := ""
	if u.User != nil {
		auth = u.User.Username()
		if password, ok := u.User.Password(); ok {
			auth += ":" + password
		}
	}
	if !clean(auth) || !clean(u.Fragment) {
		return Node{}, fail("Hysteria 2 凭据或节点名称无效")
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return Node{}, fail("查询参数格式无效")
	}
	for key, values := range q {
		if !permitted("sni insecure pinSHA256 obfs obfs-password", key) {
			return Node{}, fieldError("不支持的 Hysteria 2 查询参数", key)
		}
		if len(values) != 1 {
			return Node{}, fieldError("不支持重复查询参数", key)
		}
		if !clean(values[0]) {
			return Node{}, fail("连接参数含非法字符")
		}
	}
	if q.Has("insecure") && !permitted("0 1 false true", q.Get("insecure")) {
		return Node{}, fail("insecure 必须是 0、1、false 或 true")
	}
	tls := map[string]any{"allowInsecure": false, "alpn": []string{"h3"}}
	if q.Has("sni") {
		if q.Get("sni") == "" || strings.ContainsAny(q.Get("sni"), " /?#@\\") {
			return Node{}, fail("SNI 无效")
		}
		tls["serverName"] = q.Get("sni")
	}
	if q.Has("pinSHA256") && q.Get("pinSHA256") == "" {
		return Node{}, fail("pinSHA256 不能为空")
	}
	if err := tlsVerification(q, tls); err != nil {
		return Node{}, err
	}
	// Xray 的证书固定模式自行跳过 CA/名称验证，但仍强制匹配证书摘要。
	// 因此兼容原链接的 insecure=1 + pinSHA256，无需改写链接或启用已移除的 allowInsecure。
	if (q.Get("insecure") == "1" || q.Get("insecure") == "true") && tls["pinnedPeerCertSha256"] == nil {
		return Node{}, fail("固定 Xray 核心要求 insecure=1/true 同时提供有效 pinSHA256")
	}
	stream := map[string]any{
		"network": "hysteria", "security": "tls", "tlsSettings": tls,
		"hysteriaSettings": map[string]any{"version": 2, "auth": auth},
	}
	if q.Has("obfs") {
		if q.Get("obfs") != "salamander" {
			return Node{}, fail("Hysteria 2 混淆仅支持 salamander")
		}
		if q.Get("obfs-password") == "" {
			return Node{}, fail("Salamander 混淆密码不能为空")
		}
		stream["finalmask"] = map[string]any{"udp": []any{map[string]any{
			"type": "salamander", "settings": map[string]any{"password": q.Get("obfs-password")},
		}}}
	} else if q.Has("obfs-password") {
		return Node{}, fail("obfs-password 需要 obfs=salamander")
	}
	name := u.Fragment
	if name == "" {
		name = "hysteria2 节点"
	}
	return Node{Name: name, Protocol: "hysteria2", Outbound: map[string]any{
		"protocol": "hysteria", "settings": map[string]any{"version": 2, "address": host, "port": port},
		"streamSettings": stream,
	}}, nil
}
