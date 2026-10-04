package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ProbeOptions 指定经节点代理访问的目标；空 Method 使用 GET，零超时使用 10 秒。
type ProbeOptions struct {
	ProxyProtocol  string `json:"proxy_protocol"`
	URL            string `json:"url"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	Method         string `json:"method"`
}

type ProbeResult struct {
	OK         bool   `json:"ok"`
	StatusCode int    `json:"status_code"`
	LatencyMS  int64  `json:"latency_ms"`
	Error      string `json:"error"`
	URL        string `json:"url"`
}

// Probe 的参数及运行状态错误作为 error 返回；网络失败、目标状态作为结果返回。
// 不跟随重定向，避免探测目标悄悄变化；LatencyMS 包括最多 64 KiB 响应体的读取。
func (m *Manager) Probe(ctx context.Context, id string, opts ProbeOptions) (ProbeResult, error) {
	target, err := url.Parse(strings.TrimSpace(opts.URL))
	if err != nil || target == nil || (target.Scheme != "http" && target.Scheme != "https") || target.Hostname() == "" || target.User != nil || target.Opaque != "" || target.Fragment != "" {
		return ProbeResult{}, errors.New("探测地址必须为不含用户认证及片段的 HTTP 或 HTTPS URL")
	}
	if port := target.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return ProbeResult{}, errors.New("探测地址端口必须为 1..65535")
		}
	}
	if opts.Method != "" && opts.Method != http.MethodGet {
		return ProbeResult{}, errors.New("探测仅支持 GET 方法")
	}
	seconds := opts.TimeoutSeconds
	if seconds == 0 {
		seconds = 10
	}
	if seconds < 1 || seconds > 30 {
		return ProbeResult{}, errors.New("探测超时必须为 1..30 秒")
	}

	// 只在快照和运行状态检查时持锁，不阻塞停止节点或其他管理操作。
	m.mu.Lock()
	var record Record
	found := false
	for _, r := range m.records {
		if r.ID == id {
			record, found = r, true
			break
		}
	}
	running := alive(m.processes[id])
	closed := m.closed
	m.mu.Unlock()
	if closed {
		return ProbeResult{}, errors.New("插件正在退出")
	}
	if !found {
		return ProbeResult{}, errors.New("节点不存在")
	}
	if !running {
		return ProbeResult{}, errors.New("节点未运行，请先启动节点")
	}
	if record.Port < 1 || record.Port > 65535 {
		return ProbeResult{}, errors.New("节点本地代理端口无效")
	}
	proxyScheme := "http"
	switch record.InboundMode {
	case "", "mixed", "http":
	default:
		return ProbeResult{}, errors.New("节点入站协议无效")
	}
	if opts.ProxyProtocol != "" {
		if opts.ProxyProtocol != "http" && opts.ProxyProtocol != "socks5" {
			return ProbeResult{}, errors.New("检测代理协议仅支持 http 或 socks5")
		}
		if record.InboundMode == "http" && opts.ProxyProtocol == "socks5" {
			return ProbeResult{}, errors.New("纯 HTTP 入站不能执行 SOCKS5 检测")
		}
		proxyScheme = opts.ProxyProtocol
	}
	proxyURL := &url.URL{Scheme: proxyScheme, Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(record.Port))}
	if record.AuthEnabled {
		proxyURL.User = url.UserPassword(record.Username, record.Password)
	}
	// 固定代理，不读取环境变量；标准库支持 SOCKS5 用户密码和代理侧域名解析。
	// 再约束拨号地址，保证任何协议分支都不能直接拨号到目标。
	transport := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if addr != proxyURL.Host {
				return nil, errors.New("拒绝绕过本地代理")
			}
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
		DisableKeepAlives:      true,
		MaxResponseHeaderBytes: 64 << 10,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	defer cancel()
	result := ProbeResult{URL: target.String()}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return ProbeResult{}, errors.New("探测地址无效")
	}
	start := time.Now()
	response, requestErr := client.Do(request)
	if response != nil {
		result.StatusCode = response.StatusCode
		if requestErr == nil {
			_, requestErr = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		}
		response.Body.Close()
	}
	result.LatencyMS = time.Since(start).Milliseconds()
	if requestErr != nil {
		// 不返回底层错误：其中可能包含代理凭证或完整目标 URL。
		var networkErr net.Error
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded), errors.As(requestErr, &networkErr) && networkErr.Timeout():
			result.Error = "探测超时"
		case errors.Is(ctx.Err(), context.Canceled):
			result.Error = "探测已取消"
		default:
			result.Error = "经本地代理探测失败，请检查本地代理、节点连接及目标证书"
		}
		return result, nil
	}
	result.OK = result.StatusCode >= 200 && result.StatusCode < 400
	if !result.OK {
		result.Error = fmt.Sprintf("代理请求返回 HTTP %d", result.StatusCode)
	}
	return result, nil
}
