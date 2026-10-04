package node

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
)

// validateDownloadSettings 校验 Xray v26.3.27 的独立下载 StreamConfig，不修改输入。
// memory_settings.go 不继承目标；splithttp/dialer.go 直接解引用 Destination 并
// 将 ProtocolSettings 断言为 *splithttp.Config，因此 address、port、network 必填。
// 唯一跨流继承是上传 sockopt.penetrate=true 时的 SocketSettings；这里不开放 sockopt。
func validateDownloadSettings(value any) error {
	m, ok := value.(map[string]any)
	if !ok || m == nil {
		return fail("downloadSettings 必须是对象")
	}
	// 先限制整体大小；Marshal 同时拒绝非 JSON 值及循环引用。
	raw, err := json.Marshal(m)
	if err != nil || len(raw) > MaxLinkLength {
		return fail("downloadSettings 过长或不是有效 JSON")
	}
	for k := range m {
		if !permitted("address port network security tlsSettings realitySettings xhttpSettings splithttpSettings", k) {
			return fieldError("不支持的 downloadSettings 字段", k)
		}
	}
	address, ok := m["address"].(string)
	if !ok || len(address) > 253 {
		return fail("downloadSettings.address 必须是有效地址，不能省略")
	}
	port, ok := m["port"].(float64)
	if !ok || port < 1 || port > 65535 || port != float64(int64(port)) {
		return fail("downloadSettings.port 必须是 1 到 65535 的整数，不能省略")
	}
	if _, _, err := endpoint(address, strconv.Itoa(int(port))); err != nil {
		return fail("downloadSettings.address 无效")
	}
	network, ok := m["network"].(string)
	if !ok || !permitted("xhttp splithttp", network) {
		return fail("downloadSettings.network 必须显式指定 xhttp/splithttp")
	}
	security := "none"
	if v, exists := m["security"]; exists {
		security, ok = v.(string)
		if !ok || !permitted("none tls reality", security) {
			return fail("downloadSettings.security 仅支持 none/tls/reality")
		}
	}
	for _, key := range []string{"tlsSettings", "realitySettings"} {
		if v, exists := m[key]; exists {
			if key != security+"Settings" {
				return fieldError("下载安全设置与 security 不匹配", key)
			}
			if err := validateDownloadSecurity(v, security); err != nil {
				return err
			}
		}
	}
	if security == "reality" {
		if _, exists := m["realitySettings"]; !exists {
			return fail("下载 REALITY 必须提供 realitySettings")
		}
	}
	_, xhttp := m["xhttpSettings"]
	_, split := m["splithttpSettings"]
	if xhttp && split {
		return fail("下载 xhttpSettings 与 splithttpSettings 不能同时提供")
	}
	for _, key := range []string{"xhttpSettings", "splithttpSettings"} {
		if v, exists := m[key]; exists {
			if err := validateDownloadXHTTP(v); err != nil {
				return err
			}
		}
	}
	return nil
}

func downloadText(v any, max int) (string, bool) {
	s, ok := v.(string)
	return s, ok && len(s) <= max && clean(s)
}

func validateDownloadSecurity(value any, security string) error {
	m, ok := value.(map[string]any)
	if !ok || m == nil {
		return fail("下载 TLS/REALITY 设置必须是对象")
	}
	allowed := "serverName fingerprint"
	if security == "tls" {
		allowed += " alpn pinnedPeerCertSha256 verifyPeerCertByName allowInsecure enableSessionResumption disableSystemRoot"
	} else {
		allowed += " publicKey password shortId spiderX mldsa65Verify show"
	}
	for k, v := range m {
		if !permitted(allowed, k) {
			return fieldError("不支持的下载安全字段", k)
		}
		switch k {
		case "allowInsecure", "enableSessionResumption", "disableSystemRoot", "show":
			b, ok := v.(bool)
			if !ok || (k == "allowInsecure" && b) {
				return fieldError("下载安全字段必须是布尔值，allowInsecure 只能为 false", k)
			}
		case "alpn":
			a, ok := v.([]any)
			if !ok || len(a) > 16 {
				return fail("下载 alpn 必须是字符串数组，最多 16 项")
			}
			for _, item := range a {
				s, ok := downloadText(item, 255)
				if !ok || s == "" || !permitted("h2 http/1.1 h3", s) {
					return fail("下载 XHTTP alpn 仅支持 h2/http/1.1/h3")
				}
			}
		default:
			max := 4096
			if k == "serverName" {
				max = 253
			}
			s, ok := downloadText(v, max)
			if !ok {
				return fieldError("下载安全字段必须是长度受限的字符串", k)
			}
			switch k {
			case "fingerprint":
				fp := strings.ToLower(s)
				if (fp != "" && !permitted(fingerprints, fp)) || (security == "reality" && permitted("unsafe hellogolang", fp)) {
					return fail("下载 fingerprint 无效")
				}
			case "publicKey", "password", "mldsa65Verify":
				n := 32
				if k == "mldsa65Verify" {
					n = 1952
					if s == "" {
						continue
					}
				}
				b, err := base64.RawURLEncoding.Strict().DecodeString(s)
				if err != nil || len(b) != n {
					return fieldError("下载 REALITY 公钥格式无效", k)
				}
			case "shortId":
				if _, err := hex.DecodeString(s); err != nil || len(s) > 16 {
					return fail("下载 shortId 必须为最多 16 位偶数长度十六进制")
				}
			case "spiderX":
				if s != "" {
					u, err := url.Parse(s)
					if !strings.HasPrefix(s, "/") || err != nil || u.Host != "" {
						return fail("下载 spiderX 必须是有效的相对路径")
					}
				}
			case "pinnedPeerCertSha256":
				if s != "" {
					for _, pin := range strings.Split(s, ",") {
						b, err := hex.DecodeString(strings.ReplaceAll(strings.TrimSpace(pin), ":", ""))
						if err != nil || len(b) != 32 {
							return fail("下载 pinnedPeerCertSha256 必须是 SHA256 十六进制摘要")
						}
					}
				}
			case "verifyPeerCertByName":
				if s != "" {
					for _, name := range strings.Split(s, ",") {
						if strings.TrimSpace(name) == "" {
							return fail("下载 verifyPeerCertByName 包含空名称")
						}
					}
				}
			}
		}
	}
	if security == "reality" {
		_, pub := m["publicKey"]
		_, pass := m["password"]
		if pub == pass {
			return fail("下载 REALITY 必须且只能提供 publicKey/password 之一")
		}
	}
	return nil
}

func validateDownloadXHTTP(value any) error {
	m, ok := value.(map[string]any)
	if !ok || m == nil {
		return fail("下载 xhttpSettings 必须是对象")
	}
	// extra 在上游会替换整个 SplitHTTPConfig，仅保留 host/path/mode。
	// 不接受同级传输参数加 extra，以免核心静默丢弃同级参数。
	direct := map[string]any{}
	for k, v := range m {
		switch k {
		case "host", "path":
			max := 4096
			if k == "host" {
				max = 253
			}
			if _, ok := downloadText(v, max); !ok {
				return fieldError("下载 XHTTP 字段必须是长度受限的字符串", k)
			}
		case "mode":
			// 下载请求固定使用 stream-down；不接受看似能改变它的上传模式。
			if s, ok := v.(string); !ok || (s != "" && s != "auto") {
				return fail("下载 XHTTP mode 仅支持省略或 auto；核心固定使用 stream-down")
			}
		case "downloadSettings":
			return fail("禁止递归 downloadSettings")
		case "extra":
		default:
			direct[k] = v
		}
	}
	if extra, exists := m["extra"]; exists {
		if len(direct) > 0 {
			return fail("下载 XHTTP extra 不能与同级传输参数混用")
		}
		inner, ok := extra.(map[string]any)
		if !ok || inner == nil {
			return fail("下载 XHTTP extra 必须是对象")
		}
		// 必须先阻止递归，再调用可能接纳 downloadSettings 的上层校验器。
		if _, exists := inner["downloadSettings"]; exists {
			return fail("禁止递归 downloadSettings")
		}
		direct = inner
	}
	raw, err := json.Marshal(direct)
	if err != nil {
		return fail("下载 XHTTP extra 不是有效 JSON")
	}
	_, err = xhttpExtra(string(raw))
	return err
}
