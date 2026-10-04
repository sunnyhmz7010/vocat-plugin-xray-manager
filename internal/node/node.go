// Package node 将单个分享链接解析为 Xray v26.3.27 outbound。
// 错误消息可以指出字段名，但不能回显链接或参数值。
package node

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxLinkLength = 64 * 1024

type Node struct {
	Name     string
	Protocol string
	Outbound map[string]any
}

func permitted(allowed, key string) bool {
	for _, candidate := range strings.Fields(allowed) {
		if key == candidate {
			return true
		}
	}
	return false
}

func fail(message string) error { return errors.New(message) }

// Parse 接受一个链接；不进行网络请求，不关闭 TLS 证书验证。
func Parse(raw string) (Node, error) {
	if len(raw) > MaxLinkLength {
		return Node{}, fail("节点链接过长")
	}
	raw = strings.TrimSpace(raw)
	if raw == "" || !utf8.ValidString(raw) || strings.IndexFunc(raw, unicode.IsControl) >= 0 {
		return Node{}, fail("节点链接格式无效")
	}
	scheme, _, ok := strings.Cut(raw, "://")
	if !ok {
		return Node{}, fail("节点链接缺少协议")
	}
	switch scheme {
	case "vmess":
		return parseVMess(strings.TrimPrefix(raw, "vmess://"))
	case "vless", "trojan", "ss":
	default:
		return Node{}, fail("不支持的节点协议")
	}
	// 标准 Base64 可能包含 /，必须在 URL 解析前解码 SIP002 userinfo。
	if scheme == "ss" {
		body := strings.TrimPrefix(raw, "ss://")
		if at := strings.Index(body, "@"); at >= 0 && !strings.Contains(body[:at], ":") {
			encoded, err := url.PathUnescape(body[:at])
			if err != nil {
				return Node{}, fail("Shadowsocks 凭据编码无效")
			}
			b, err := decode64(encoded)
			if err != nil {
				return Node{}, fail("Shadowsocks 凭据编码无效")
			}
			method, password, ok := strings.Cut(string(b), ":")
			if !ok {
				return Node{}, fail("Shadowsocks 凭据格式无效")
			}
			raw = "ss://" + url.UserPassword(method, password).String() + body[at:]
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" || u.User == nil || (u.Path != "" && u.Path != "/") {
		return Node{}, fail("节点链接格式无效")
	}
	if strings.Count(u.Host, ":") > 1 && !strings.HasPrefix(u.Host, "[") {
		return Node{}, fail("IPv6 地址必须使用方括号")
	}
	host, port, err := endpoint(u.Hostname(), u.Port())
	if err != nil {
		return Node{}, err
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return Node{}, fail("查询参数格式无效")
	}
	for key, values := range q {
		if len(values) != 1 {
			return Node{}, fieldError("不支持重复查询参数", key)
		}
	}
	name := u.Fragment
	if name == "" {
		name = scheme + " 节点"
	}
	if !clean(name) {
		return Node{}, fail("节点名称无效")
	}
	if scheme == "ss" {
		return parseSS(u, q, name, host, port)
	}
	allowed := " type security sni fp alpn allowInsecure path host serviceName mode headerType authority ed extra seed pcs pinnedPeerCertSha256 pinSHA256 vcn verifyPeerCertByName verifyPeerCertInNames "
	if scheme == "vless" {
		allowed += " encryption flow pbk sid spx packetEncoding pqv mldsa65Verify "
	}
	for key := range q {
		if !permitted(allowed, key) {
			return Node{}, fieldError("不支持的查询参数", key)
		}
	}
	if _, present := q["allowInsecure"]; present && q.Get("allowInsecure") != "0" && q.Get("allowInsecure") != "false" {
		return Node{}, fail("allowInsecure=true 已被固定 Xray 核心移除；请使用 pcs/pinnedPeerCertSha256 和 vcn/verifyPeerCertByName")
	}
	credential := u.User.Username()
	if _, hasPassword := u.User.Password(); hasPassword || credential == "" || !clean(credential) {
		return Node{}, fail("节点凭据格式无效")
	}
	var settings map[string]any
	if scheme == "vless" {
		if !uuid(credential) {
			return Node{}, fail("UUID 格式无效")
		}
		encryption := "none"
		if q.Has("encryption") {
			encryption = q.Get("encryption")
		}
		if !validEncryption(encryption) {
			return Node{}, fail("不支持或无效的 VLESS encryption")
		}
		if q.Has("packetEncoding") && q.Get("packetEncoding") != "none" && q.Get("packetEncoding") != "xudp" {
			return Node{}, fail("packetEncoding 仅支持 none/xudp；packet 无 Xray 实现")
		}
		user := map[string]any{"id": credential, "encryption": encryption}
		if flow := q.Get("flow"); flow != "" {
			if flow != "xtls-rprx-vision" && flow != "xtls-rprx-vision-udp443" {
				return Node{}, fail("不支持的 VLESS flow")
			}
			user["flow"] = flow
		}
		settings = map[string]any{"vnext": []any{map[string]any{"address": host, "port": port, "users": []any{user}}}}
	} else {
		if !q.Has("security") {
			q.Set("security", "tls")
		}
		settings = map[string]any{"servers": []any{map[string]any{"address": host, "port": port, "password": credential}}}
	}
	stream, err := streamSettings(q, scheme)
	if err != nil {
		return Node{}, err
	}
	outbound := map[string]any{"protocol": scheme, "settings": settings, "streamSettings": stream}
	if q.Get("packetEncoding") == "xudp" {
		outbound["mux"] = map[string]any{"enabled": true, "concurrency": -1, "xudpConcurrency": 16, "xudpProxyUDP443": "allow"}
	}
	return Node{Name: name, Protocol: scheme, Outbound: outbound}, nil
}

func endpoint(host, portText string) (string, int, error) {
	if host == "" || !clean(host) || strings.ContainsAny(host, " /?#@\\%[]") {
		return "", 0, fail("服务器地址无效")
	}
	if strings.Contains(host, ":") && net.ParseIP(host) == nil {
		return "", 0, fail("IPv6 地址无效")
	}
	if portText == "" || strings.IndexFunc(portText, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return "", 0, fail("端口必须为 1 到 65535 的整数")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, fail("端口必须为 1 到 65535 的整数")
	}
	return host, port, nil
}

func clean(s string) bool { return utf8.ValidString(s) && strings.IndexFunc(s, unicode.IsControl) < 0 }
func uuid(s string) bool {
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return false
	}
	b, err := hex.DecodeString(strings.ReplaceAll(s, "-", ""))
	return err == nil && len(b) == 16
}
func decode64(s string) ([]byte, error) {
	if strings.ContainsAny(s, "\r\n\t ") {
		return nil, fail("Base64 编码无效")
	}
	for _, enc := range []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.StdEncoding} {
		if b, err := enc.Strict().DecodeString(s); err == nil {
			return b, nil
		}
	}
	return nil, fail("Base64 编码无效")
}

func streamSettings(q url.Values, protocol string) (map[string]any, error) {
	for _, vs := range q {
		for _, v := range vs {
			if !clean(v) {
				return nil, fail("连接参数含非法字符")
			}
		}
	}
	network := q.Get("type")
	switch network {
	case "websocket":
		network = "ws"
	case "splithttp":
		network = "xhttp"
	case "mkcp":
		network = "kcp"
	}
	if network == "" || network == "tcp" {
		network = "raw"
	}
	security := q.Get("security")
	if security == "" {
		security = "none"
	}
	switch network {
	case "raw", "ws", "grpc", "xhttp", "httpupgrade", "kcp":
	default:
		return nil, fail("不支持的传输方式")
	}
	switch security {
	case "none", "tls":
	case "reality":
		if protocol != "vless" || (network != "raw" && network != "xhttp" && network != "grpc") {
			return nil, fail("不支持的 Reality 组合")
		}
	default:
		return nil, fail("不支持的传输安全方式")
	}
	if q.Get("flow") != "" && (network != "raw" || security == "none") {
		return nil, fail("Vision flow 需要 RAW 和 TLS 或 Reality")
	}
	transport, err := transportSettings(q, network)
	if err != nil {
		return nil, err
	}
	s := map[string]any{"network": network, "security": security}
	if transport != nil {
		s[network+"Settings"] = transport
	}

	if network == "kcp" {
		mask, err := kcpMask(q)
		if err != nil {
			return nil, err
		}
		if mask != nil {
			s["finalmask"] = mask
		}
	}
	if security == "none" {
		for _, key := range []string{"sni", "fp", "alpn", "pbk", "sid", "spx", "pqv", "mldsa65Verify", "pcs", "pinnedPeerCertSha256", "pinSHA256", "vcn", "verifyPeerCertByName", "verifyPeerCertInNames"} {
			if q.Has(key) {
				return nil, fail("安全参数需要 TLS 或 Reality")
			}
		}
		return s, nil
	}
	t := map[string]any{}
	if q.Has("sni") {
		if strings.ContainsAny(q.Get("sni"), " /?#@\\") {
			return nil, fail("SNI 无效")
		}
		t["serverName"] = q.Get("sni")
	}
	fp := strings.ToLower(q.Get("fp"))
	if fp != "" {
		if !permitted(fingerprints, fp) || (security == "reality" && (fp == "unsafe" || fp == "hellogolang")) {
			return nil, fail("不支持的 TLS fingerprint")
		}
		t["fingerprint"] = fp
	}
	if security == "tls" {
		for _, key := range []string{"pbk", "sid", "spx", "pqv", "mldsa65Verify"} {
			if q.Has(key) {
				return nil, fail("Reality 参数需要 Reality")
			}
		}
		if err := tlsVerification(q, t); err != nil {
			return nil, err
		}
		t["allowInsecure"] = false
		if q.Has("alpn") {
			alpn := strings.Split(q.Get("alpn"), ",")
			for _, a := range alpn {
				if len(a) == 0 || len(a) > 255 || strings.TrimSpace(a) != a {
					return nil, fail("ALPN 无效")
				}
			}
			t["alpn"] = alpn
		}
	} else {
		for _, k := range []string{"pcs", "pinnedPeerCertSha256", "pinSHA256", "vcn", "verifyPeerCertByName", "verifyPeerCertInNames"} {
			if q.Has(k) {
				return nil, fieldError("字段仅适用于 TLS", k)
			}
		}
		pqv, err := aliasValue(q, "pqv", "mldsa65Verify")
		if err != nil {
			return nil, err
		}
		if pqv != "" {
			b, e := base64.RawURLEncoding.Strict().DecodeString(pqv)
			if e != nil || len(b) != 1952 {
				return nil, fail("pqv/mldsa65Verify 必须是 1952 字节 RawURLBase64 公钥")
			}
			t["mldsa65Verify"] = pqv
		}
		if q.Has("alpn") {
			return nil, fail("Reality 不支持 ALPN 参数")
		}
		key, err := decode64(q.Get("pbk"))
		if err != nil || len(key) != 32 {
			return nil, fail("Reality 公钥必须是 32 字节 Base64")
		}
		t["password"] = base64.RawURLEncoding.EncodeToString(key)
		sid := q.Get("sid")
		decoded, err := hex.DecodeString(sid)
		if err != nil || len(decoded) > 8 {
			return nil, fail("Reality shortId 必须是最多 16 位偶数长度十六进制")
		}
		t["shortId"] = sid
		if fp == "" {
			t["fingerprint"] = "chrome"
		}
		if q.Has("spx") {
			t["spiderX"] = q.Get("spx")
		}
	}
	s[security+"Settings"] = t
	return s, nil
}

func parseSS(u *url.URL, q url.Values, name, host string, port int) (Node, error) {
	if q.Has("plugin") {
		return Node{}, fail("不支持 Shadowsocks SIP002 插件")
	}
	for key := range q {
		return Node{}, fieldError("不支持的 Shadowsocks 查询参数", key)
	}
	method, password := u.User.Username(), ""
	if p, ok := u.User.Password(); ok {
		password = p
	} else {
		b, err := decode64(method)
		if err != nil {
			return Node{}, fail("Shadowsocks 凭据编码无效")
		}
		var ok bool
		method, password, ok = strings.Cut(string(b), ":")
		if !ok {
			return Node{}, fail("Shadowsocks 凭据格式无效")
		}
	}
	switch method {
	case "aes-128-gcm", "aes-256-gcm", "chacha20-ietf-poly1305", "2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305":
	default:
		return Node{}, fail("不支持的 Shadowsocks 加密方式")
	}
	if password == "" || !clean(password) {
		return Node{}, fail("Shadowsocks 密码无效")
	}
	if strings.HasPrefix(method, "2022-") {
		size := 32
		if method == "2022-blake3-aes-128-gcm" {
			size = 16
		}
		keys := strings.Split(password, ":")
		for i, k := range keys {
			b, err := decode64(k)
			if err != nil || len(b) != size {
				return Node{}, fail("Shadowsocks 2022 密钥长度或编码无效")
			}
			keys[i] = base64.StdEncoding.EncodeToString(b)
		}
		password = strings.Join(keys, ":")
	}
	return Node{Name: name, Protocol: "shadowsocks", Outbound: map[string]any{"protocol": "shadowsocks", "settings": map[string]any{"servers": []any{map[string]any{"address": host, "port": port, "method": method, "password": password}}}}}, nil
}

func parseVMess(payload string) (Node, error) {
	b, err := decode64(payload)
	if err != nil {
		return Node{}, err
	}
	// Token 级解码同时拒绝重复字段，避免不同客户端解释不一致。
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.UseNumber()
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return Node{}, fail("VMess JSON 无效")
	}
	fields := map[string]string{}
	allowed := " v ps add port id aid scy net type host path tls sni alpn fp allowInsecure serviceName mode "
	for d.More() {
		t, e := d.Token()
		if e != nil {
			return Node{}, fail("VMess JSON 无效")
		}
		k, ok := t.(string)
		if !ok {
			return Node{}, fail("VMess JSON 无效")
		}
		if _, exists := fields[k]; exists {
			return Node{}, fail("VMess JSON 字段重复")
		}
		if !permitted(allowed, k) {
			return Node{}, fieldError("不支持的 VMess 字段", k)
		}
		var v any
		if d.Decode(&v) != nil {
			return Node{}, fail("VMess JSON 无效")
		}
		switch value := v.(type) {
		case string:
			fields[k] = value
		case json.Number:
			if k != "port" && k != "aid" && k != "v" {
				return Node{}, fail("VMess 字段类型无效")
			}
			fields[k] = value.String()
		case bool:
			if k != "allowInsecure" {
				return Node{}, fail("VMess 字段类型无效")
			}
			fields[k] = strconv.FormatBool(value)
		default:
			return Node{}, fail("VMess 字段类型无效")
		}
		if !clean(fields[k]) {
			return Node{}, fail("VMess 字段含非法字符")
		}
	}
	if _, err = d.Token(); err != nil {
		return Node{}, fail("VMess JSON 无效")
	}
	if _, err = d.Token(); err != io.EOF {
		return Node{}, fail("VMess JSON 含多余内容")
	}
	if fields["v"] != "" && fields["v"] != "2" {
		return Node{}, fail("不支持的 VMess 分享版本")
	}
	if fields["aid"] != "" && fields["aid"] != "0" {
		return Node{}, fail("仅支持 VMess AEAD（alterId=0）")
	}
	if v, ok := fields["allowInsecure"]; ok && v != "false" && v != "0" {
		return Node{}, fail("allowInsecure=true 已被固定 Xray 核心移除；请使用 pcs/pinnedPeerCertSha256 和 vcn/verifyPeerCertByName")
	}
	host, port, err := endpoint(strings.TrimSuffix(strings.TrimPrefix(fields["add"], "["), "]"), fields["port"])
	if err != nil {
		return Node{}, err
	}
	if !uuid(fields["id"]) {
		return Node{}, fail("UUID 格式无效")
	}
	cipher := fields["scy"]
	if cipher == "" {
		cipher = "auto"
	}
	switch cipher {
	case "auto", "aes-128-gcm", "chacha20-poly1305", "none", "zero":
	default:
		return Node{}, fail("不支持的 VMess encryption")
	}
	q := url.Values{"type": {fields["net"]}, "security": {fields["tls"]}}
	for _, key := range []string{"sni", "alpn", "fp", "host", "path", "serviceName", "mode"} {
		if fields[key] != "" {
			q.Set(key, fields[key])
		}
	}
	if fields["type"] != "" {
		q.Set("headerType", fields["type"])
	}
	if fields["net"] == "grpc" && !q.Has("serviceName") && q.Has("path") {
		q.Set("serviceName", q.Get("path"))
		q.Del("path")
	}
	stream, err := streamSettings(q, "vmess")
	if err != nil {
		return Node{}, err
	}
	name := fields["ps"]
	if name == "" {
		name = "vmess 节点"
	}
	return Node{Name: name, Protocol: "vmess", Outbound: map[string]any{"protocol": "vmess", "settings": map[string]any{"vnext": []any{map[string]any{"address": host, "port": port, "users": []any{map[string]any{"id": fields["id"], "alterId": 0, "security": cipher}}}}}, "streamSettings": stream}}, nil
}
