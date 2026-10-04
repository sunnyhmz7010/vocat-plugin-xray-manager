package node

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
)

// 固定于 Xray v26.3.27 transport/internet/tls/tls.go。
const fingerprints = `chrome firefox safari ios android edge 360 qq random randomized randomizednoalpn unsafe hellogolang hellorandomized hellorandomizedalpn hellorandomizednoalpn hellofirefox_auto hellofirefox_55 hellofirefox_56 hellofirefox_63 hellofirefox_65 hellofirefox_99 hellofirefox_102 hellofirefox_105 hellofirefox_120 hellochrome_auto hellochrome_58 hellochrome_62 hellochrome_70 hellochrome_72 hellochrome_83 hellochrome_87 hellochrome_96 hellochrome_100 hellochrome_102 hellochrome_106_shuffle hellochrome_120 hellochrome_131 helloios_auto helloios_11_1 helloios_12_1 helloios_13 helloios_14 helloandroid_11_okhttp helloedge_auto helloedge_85 helloedge_106 hellosafari_auto hellosafari_16_0 hello360_auto hello360_7_5 hello360_11_0 helloqq_auto helloqq_11_1 hellochrome_100_psk hellochrome_112_psk_shuf hellochrome_114_padding_psk_shuf hellochrome_115_pq hellochrome_115_pq_psk hellochrome_120_pq`

func fieldError(message, key string) error {
	// 字段名也是不可信输入：转义控制字符，并限制长度。
	if len(key) > 80 {
		key = key[:80] + "…"
	}
	return fail(message + "：" + strconv.QuoteToASCII(key))
}

func aliasValue(q url.Values, keys ...string) (string, error) {
	value, found := "", false
	for _, k := range keys {
		if q.Has(k) {
			if found {
				return "", fieldError("别名参数不能同时提供", k)
			}
			value, found = q.Get(k), true
		}
	}
	return value, nil
}

func validEncryption(s string) bool {
	if s == "none" {
		return true
	}
	parts := strings.Split(s, ".")
	if len(parts) < 4 || parts[0] != "mlkem768x25519plus" || !permitted("native xorpub random", parts[1]) || !permitted("0rtt 1rtt", parts[2]) {
		return false
	}
	keys, paddingCount, maxPadding := 0, 0, 0
	for _, part := range parts[3:] {
		// 与核心 encryption.ParsePadding 的三元组及总长度约束保持一致。
		if len(part) < 20 {
			if keys != 0 || part == "" {
				return false
			}
			tuple := strings.Split(part, "-")
			if len(tuple) != 3 {
				return false
			}
			values := [3]int{}
			for i, p := range tuple {
				n, e := strconv.Atoi(p)
				if e != nil || n < 0 {
					return false
				}
				values[i] = n
			}
			if values[0] > 100 || values[1] > values[2] {
				return false
			}
			if paddingCount == 0 && (values[0] != 100 || values[1] < 35) {
				return false
			}
			if paddingCount%2 == 0 {
				if values[2] > 65553-maxPadding {
					return false
				}
				maxPadding += values[2]
			}
			paddingCount++
			continue
		}
		b, err := base64.RawURLEncoding.Strict().DecodeString(part)
		if err != nil || (len(b) != 32 && len(b) != 1184) {
			return false
		}
		keys++
	}
	return keys > 0
}

func tlsVerification(q url.Values, t map[string]any) error {
	if q.Has("verifyPeerCertInNames") {
		return fail("verifyPeerCertInNames 已移除；请使用 vcn/verifyPeerCertByName")
	}
	pin, err := aliasValue(q, "pcs", "pinnedPeerCertSha256", "pinSHA256")
	if err != nil {
		return err
	}
	if pin != "" {
		values := strings.Split(pin, ",")
		for i, v := range values {
			b, e := hex.DecodeString(strings.ReplaceAll(strings.TrimSpace(v), ":", ""))
			if e != nil || len(b) != 32 {
				return fail("pcs/pinnedPeerCertSha256/pinSHA256 必须是 SHA256 十六进制摘要")
			}
			values[i] = hex.EncodeToString(b)
		}
		t["pinnedPeerCertSha256"] = strings.Join(values, ",")
	}
	names, err := aliasValue(q, "vcn", "verifyPeerCertByName")
	if err != nil {
		return err
	}
	if names != "" {
		for _, name := range strings.Split(names, ",") {
			if strings.TrimSpace(name) == "" {
				return fail("vcn/verifyPeerCertByName 包含空名称")
			}
		}
		t["verifyPeerCertByName"] = names
	}
	return nil
}

func transportSettings(q url.Values, network string) (map[string]any, error) {
	allowed := "headerType"
	switch network {
	case "raw":
		allowed += " path host"
	case "ws", "httpupgrade":
		allowed += " path host ed"
	case "xhttp":
		allowed += " path host mode extra"
	case "grpc":
		allowed += " serviceName mode authority"
	case "kcp":
		allowed += " seed"
	}
	for _, key := range []string{"path", "host", "serviceName", "mode", "authority", "ed", "extra", "seed"} {
		if q.Has(key) && !permitted(allowed, key) {
			return nil, fieldError("该传输不支持字段", key)
		}
	}
	header := q.Get("headerType")
	if network != "raw" && network != "kcp" && header != "" && header != "none" {
		return nil, fail("此传输不支持 headerType；请在 RAW 或 KCP 中设置")
	}
	t := map[string]any{}
	switch network {
	case "raw":
		if header != "" && header != "none" && header != "http" {
			return nil, fail("RAW headerType 仅支持 none/http")
		}
		if header != "http" {
			if q.Has("path") || q.Has("host") {
				return nil, fail("RAW path/host 需要 headerType=http")
			}
			return nil, nil
		}
		request := map[string]any{}
		if q.Has("path") {
			request["path"] = strings.Split(q.Get("path"), ",")
		}
		if q.Has("host") {
			request["headers"] = map[string]any{"Host": strings.Split(q.Get("host"), ",")}
		}
		t["header"] = map[string]any{"type": "http", "request": request}
	case "kcp":
		// seed 和头部封装由 streamSettings 写入 finalmask。
	case "grpc":
		t["serviceName"] = q.Get("serviceName")
		if q.Has("authority") {
			t["authority"] = q.Get("authority")
		}
		switch q.Get("mode") {
		case "", "gun":
		case "multi":
			t["multiMode"] = true
		default:
			return nil, fail("不支持的 gRPC mode")
		}
	case "ws", "httpupgrade", "xhttp":
		if q.Has("path") {
			t["path"] = q.Get("path")
		}
		if q.Has("host") {
			t["host"] = q.Get("host")
		}
		if q.Has("ed") {
			n, err := strconv.ParseUint(q.Get("ed"), 10, 32)
			if err != nil {
				return nil, fail("ed 必须是 uint32 整数")
			}
			path := q.Get("path")
			if path == "" {
				path = "/"
			}
			u, err := url.Parse(path)
			if err != nil {
				return nil, fail("ed 对应 path 无效")
			}
			query := u.Query()
			if query.Has("ed") {
				return nil, fail("ed 与 path 中的 ed 重复")
			}
			query.Set("ed", strconv.FormatUint(n, 10))
			u.RawQuery = query.Encode()
			t["path"] = u.String()
		}
		if network == "xhttp" {
			mode := q.Get("mode")
			if mode == "" {
				mode = "auto"
			}
			if !permitted("auto packet-up stream-up stream-one", mode) {
				return nil, fail("不支持的 XHTTP mode")
			}
			t["mode"] = mode
			if q.Has("extra") {
				extra, err := xhttpExtra(q.Get("extra"))
				if err != nil {
					return nil, err
				}
				if _, hasDownload := extra["downloadSettings"]; hasDownload && mode == "stream-one" {
					return nil, fail("XHTTP stream-one 不能使用独立 downloadSettings，请选择 packet-up、stream-up 或 auto")
				}
				t["extra"] = extra
			}
		}
	}
	return t, nil
}

// extra 只接纳已核实的传输字段和独立下载设置，不接受 outbound 或 sockopt。
func xhttpExtra(raw string) (map[string]any, error) {
	var extra map[string]any
	if !uniqueJSONFields(raw) || json.Unmarshal([]byte(raw), &extra) != nil || extra == nil {
		return nil, fail("XHTTP extra 必须是 JSON 对象")
	}
	allowed := "headers xPaddingBytes xPaddingObfsMode xPaddingKey xPaddingHeader xPaddingPlacement xPaddingMethod uplinkHTTPMethod sessionPlacement sessionKey seqPlacement seqKey uplinkDataPlacement uplinkDataKey uplinkChunkSize noGRPCHeader noSSEHeader scMaxEachPostBytes scMinPostsIntervalMs scMaxBufferedPosts scStreamUpServerSecs serverMaxHeaderBytes xmux downloadSettings"
	for k, v := range extra {
		if !permitted(allowed, k) {
			return nil, fieldError("不支持的 XHTTP extra 字段", k)
		}
		if k == "downloadSettings" {
			if err := validateDownloadSettings(v); err != nil {
				return nil, err
			}
			continue
		}
		if k == "xmux" {
			m, ok := v.(map[string]any)
			if !ok {
				return nil, fail("extra.xmux 必须是对象")
			}
			for key, value := range m {
				if !permitted("maxConcurrency maxConnections cMaxReuseTimes hMaxRequestTimes hMaxReusableSecs hKeepAlivePeriod", key) {
					return nil, fieldError("不支持的 extra.xmux 字段", key)
				}
				if !validRange(value) {
					return nil, fieldError("extra.xmux 数值或范围无效", key)
				}
			}
		}
		switch {
		case permitted("noGRPCHeader noSSEHeader xPaddingObfsMode", k):
			if _, ok := v.(bool); !ok {
				return nil, fieldError("extra 字段必须是布尔值", k)
			}
		case permitted("xPaddingBytes uplinkChunkSize scMaxEachPostBytes scMinPostsIntervalMs scStreamUpServerSecs", k):
			if !validRange(v) {
				return nil, fieldError("extra 范围无效", k)
			}
		case permitted("scMaxBufferedPosts serverMaxHeaderBytes", k):
			if n, ok := v.(float64); !ok || n < 0 || n > 2147483647 || n != float64(int64(n)) {
				return nil, fieldError("extra 字段必须是非负整数", k)
			}
		case k != "headers" && k != "xmux":
			if text, ok := v.(string); !ok || !clean(text) {
				return nil, fieldError("extra 字段必须是字符串", k)
			}
		}
		if k == "headers" {
			m, ok := v.(map[string]any)
			if !ok {
				return nil, fail("extra.headers 必须是对象")
			}
			for key, value := range m {
				text, ok := value.(string)
				if !ok || !clean(text) || !clean(key) || strings.EqualFold(key, "host") {
					return nil, fail("extra.headers 无效；Host 请使用 host 参数")
				}
			}
		}
	}
	return extra, nil
}

func validRange(v any) bool {
	if n, ok := v.(float64); ok {
		return n >= 0 && n <= 2147483647 && n == float64(int64(n))
	}
	text, ok := v.(string)
	if !ok {
		return false
	}
	parts := strings.Split(text, "-")
	if len(parts) > 2 {
		return false
	}
	last := int64(-1)
	for _, p := range parts {
		n, e := strconv.ParseInt(p, 10, 32)
		if e != nil || n < 0 || n < last {
			return false
		}
		last = n
	}
	return true
}

func uniqueJSONFields(raw string) bool { return uniqueJSONFieldsDepth(raw, 0) }
func uniqueJSONFieldsDepth(raw string, depth int) bool {
	if depth > 16 {
		return false
	}
	d := json.NewDecoder(strings.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return false
	}
	seen := map[string]bool{}
	for d.More() {
		token, err = d.Token()
		if err != nil {
			return false
		}
		key, ok := token.(string)
		if !ok || seen[key] {
			return false
		}
		seen[key] = true
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return false
		}
		trim := strings.TrimSpace(string(value))
		if strings.HasPrefix(trim, "{") && !uniqueJSONFieldsDepth(trim, depth+1) {
			return false
		}
	}
	token, err = d.Token()
	return err == nil && token == json.Delim('}')
}
