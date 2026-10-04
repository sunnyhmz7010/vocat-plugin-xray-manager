package node

import "net/url"

// kcpMask 将分享链接的 seed/headerType 转换成固定内核的 finalmask。
// 列表从外层到内层：UDP 包先添加头部，再包裹 KCP 校验/加密层；发送时逆序执行。
func kcpMask(q url.Values) (map[string]any, error) {
	header := q.Get("headerType")
	headers := map[string]string{"": "", "none": "", "srtp": "header-srtp", "utp": "header-utp", "wechat-video": "header-wechat", "dtls": "header-dtls", "wireguard": "header-wireguard"}
	mask, ok := headers[header]
	if !ok {
		return nil, fail("KCP headerType 仅支持 none/srtp/utp/wechat-video/dtls/wireguard")
	}
	// 无这两个分享参数时保留核心原生 KCP，避免改变已有原生连接。
	if !q.Has("seed") && !q.Has("headerType") {
		return nil, nil
	}
	layers := make([]any, 0, 2)
	if mask != "" {
		layers = append(layers, map[string]any{"type": mask})
	}
	if q.Has("seed") {
		layers = append(layers, map[string]any{"type": "mkcp-aes128gcm", "settings": map[string]any{"password": q.Get("seed")}})
	} else {
		layers = append(layers, map[string]any{"type": "mkcp-original"})
	}
	return map[string]any{"udp": layers}, nil
}
