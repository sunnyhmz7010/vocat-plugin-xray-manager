// Package engine 托管每个节点的 Xray 进程及固定本地代理端口。
package engine

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"vocat-plugin-xray-manager/internal/node"
)

type Record struct {
	InboundMode       string   `json:"inbound_mode"`
	UDPDisabled       bool     `json:"udp_disabled,omitempty"`
	AllowLAN          bool     `json:"allow_lan"`
	AuthEnabled       bool     `json:"auth_enabled"`
	PreviousUsernames []string `json:"previous_usernames,omitempty"`
	PreviousPorts     []int    `json:"previous_ports,omitempty"`
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Protocol          string   `json:"protocol"`
	Link              string   `json:"link"`
	Port              int      `json:"port"`
	Username          string   `json:"username"`
	Password          string   `json:"password"`
	Enabled           bool     `json:"enabled"`
}
type View struct {
	InboundMode       string   `json:"inbound_mode"`
	UDPEnabled        bool     `json:"udp_enabled"`
	AllowLAN          bool     `json:"allow_lan"`
	AuthEnabled       bool     `json:"auth_enabled"`
	Username          string   `json:"username"`
	PasswordSet       bool     `json:"password_set"`
	PreviousUsernames []string `json:"previous_usernames"`
	PreviousAddrs     []string `json:"previous_addrs,omitempty"`
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Protocol          string   `json:"protocol"`
	Addr              string   `json:"addr"`
	Enabled           bool     `json:"enabled"`
	Running           bool     `json:"running"`
	Error             string   `json:"error,omitempty"`
}
type Export struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Addr     string `json:"addr"`
	Username string `json:"username"`
	Password string `json:"password"`
	Enabled  bool   `json:"enabled"`
}
type process struct {
	cmd  *exec.Cmd
	done chan struct{}
}
type Manager struct {
	mu        sync.Mutex
	dir, core string
	records   []Record
	processes map[string]*process
	failures  map[string]string
	closed    bool
}

func Open(dir, core string) (*Manager, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	m := &Manager{dir: dir, core: core, processes: map[string]*process{}, failures: map[string]string{}}
	data, err := os.ReadFile(filepath.Join(dir, "nodes.json"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		if err := json.Unmarshal(data, &m.records); err != nil {
			return nil, errors.New("节点存储损坏，拒绝覆盖原文件")
		}
		seen := map[string]bool{}
		ports := map[int]bool{}
		for _, r := range m.records {
			if !validInboundMode(normalizedInboundMode(r.InboundMode)) {
				return nil, errors.New("节点入站模式无效")
			}
			if !validID(r.ID) || seen[r.ID] || ports[r.Port] || r.Port < 1 || r.Port > 65535 || len(r.Username) > 255 || len(r.Password) > 255 {
				return nil, errors.New("节点存储字段无效")
			}
			for _, port := range r.PreviousPorts {
				if port < 1 || port > 65535 {
					return nil, errors.New("节点历史端口无效")
				}
			}
			if (r.AuthEnabled && (!validCredential(r.Username) || !validCredential(r.Password))) || (!r.AuthEnabled && (r.Username != "" || r.Password != "")) {
				return nil, errors.New("节点认证字段无效")
			}
			for _, username := range r.PreviousUsernames {
				if !validCredential(username) {
					return nil, errors.New("节点历史用户名无效")
				}
			}
			seen[r.ID] = true
			ports[r.Port] = true
		}
	}
	// 单个节点失败不妨碍其他节点或管理面板启动。
	for _, r := range m.records {
		if r.Enabled {
			if err := m.start(r); err != nil {
				m.failures[r.ID] = err.Error()
			}
		}
	}
	return m, nil
}

const nodeIDPrefix = "xray-manager-"

func validID(id string) bool {
	if len(id) != len(nodeIDPrefix)+16 || !strings.HasPrefix(id, nodeIDPrefix) {
		return false
	}
	_, err := hex.DecodeString(id[len(nodeIDPrefix):])
	return err == nil
}
func alive(p *process) bool {
	if p == nil {
		return false
	}
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}
func (m *Manager) view(r Record) View {
	running := alive(m.processes[r.ID])
	problem := m.failures[r.ID]
	if r.Enabled && !running && problem == "" {
		problem = "内核已退出，请点击启动重试"
	}
	previous := make([]string, 0, len(r.PreviousPorts))
	for _, port := range r.PreviousPorts {
		previous = append(previous, net.JoinHostPort("127.0.0.1", fmt.Sprint(port)))
	}
	username := ""
	if r.AuthEnabled {
		username = r.Username
	}
	return View{InboundMode: normalizedInboundMode(r.InboundMode), UDPEnabled: recordUDPEnabled(r), AllowLAN: r.AllowLAN, AuthEnabled: r.AuthEnabled, Username: username, PasswordSet: r.AuthEnabled && r.Password != "", PreviousUsernames: append([]string{}, r.PreviousUsernames...), ID: r.ID, Name: r.Name, Protocol: r.Protocol, Addr: net.JoinHostPort("127.0.0.1", fmt.Sprint(r.Port)), Enabled: r.Enabled, Running: running, Error: problem, PreviousAddrs: previous}
}
func (m *Manager) List() []View {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]View, 0, len(m.records))
	for _, r := range m.records {
		out = append(out, m.view(r))
	}
	return out
}

// AddWithSettings 的 port=0 自动分配，其余连接设置显式指定。
func (m *Manager) AddWithSettings(link string, settings Settings) (View, error) {
	port := settings.Port
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return View{}, errors.New("插件正在退出")
	}
	if port < 0 || port > 65535 {
		return View{}, errors.New("本地端口必须为 0（自动）或 1..65535")
	}
	link = strings.TrimSpace(link)
	n, err := node.Parse(link)
	if err != nil {
		return View{}, err
	}
	hash := sha256.Sum256([]byte(link))
	id := nodeIDPrefix + hex.EncodeToString(hash[:8])
	for _, r := range m.records {
		if r.ID == id && r.Link != link {
			return View{}, errors.New("原链接对应的节点已修改参数，请编辑该节点或为新链接添加不同备注")
		}
		if r.Link == link {
			if port != 0 && port != r.Port {
				return View{}, errors.New("该链接已存在且端口不同，请在连接设置中修改端口")
			}
			candidate, err := applySettings(r, settings)
			if err != nil {
				return View{}, err
			}
			if !sameSettings(r, candidate) {
				return View{}, errors.New("该链接已存在且设置不同，请使用连接设置")
			}
			return m.view(r), nil
		}
	}
	validated, err := applySettings(Record{}, settings)
	if err != nil {
		return View{}, err
	}
	if len(m.records) >= 64 {
		return View{}, errors.New("最多保存 64 个节点")
	}
	if port == 0 {
		reserved := make([]int, 0, len(m.records))
		for _, r := range m.records {
			reserved = append(reserved, r.Port)
		}
		port, err = availablePortForUDP(settings.AllowLAN, recordUDPEnabled(validated), reserved...)
		if err != nil {
			return View{}, errors.New("无法分配本地 TCP/UDP 端口")
		}
	} else if err := m.checkRecordBinding(validated); err != nil {
		return View{}, err
	}
	r := validated
	r.ID, r.Name, r.Protocol, r.Link, r.Port, r.Enabled = id, n.Name, n.Protocol, link, port, true
	if err := m.start(r); err != nil {
		return View{}, err
	}
	next := append(append([]Record{}, m.records...), r)
	if err := m.save(next); err != nil {
		m.stop(id)
		return View{}, err
	}
	m.records = next
	return m.view(r), nil
}

func (m *Manager) checkPort(port int, exceptID string) error {
	return m.checkBinding(port, exceptID, false)
}
func (m *Manager) checkBinding(port int, exceptID string, allowLAN bool) error {
	return m.checkRecordBinding(Record{Port: port, ID: exceptID, AllowLAN: allowLAN})
}
func (m *Manager) checkRecordBinding(record Record) error {
	port, exceptID := record.Port, record.ID
	for _, r := range m.records {
		if r.ID != exceptID && r.Port == port {
			return fmt.Errorf("本地端口 %d 已被其他已保存节点预留", port)
		}
	}
	return checkAvailableBindingUDP(port, record.AllowLAN, recordUDPEnabled(record))
}

func checkAvailablePort(port int) error {
	return checkAvailableBinding(port, false)
}
func checkAvailableBinding(port int, allowLAN bool) error {
	return checkAvailableBindingUDP(port, allowLAN, true)
}
func checkAvailableBindingUDP(port int, allowLAN, udp bool) error {
	addr := net.JoinHostPort(listenHost(allowLAN), fmt.Sprint(port))
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("本地 TCP 端口 %d 不可用", port)
	}
	defer l.Close()
	if !udp {
		return nil
	}
	u, err := net.ListenPacket("udp", addr)
	if err != nil {
		return fmt.Errorf("本地 UDP 端口 %d 不可用", port)
	}
	return u.Close()
}

func (m *Manager) SetEnabled(id string, enabled bool) (View, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return View{}, errors.New("插件正在退出")
	}
	for i, r := range m.records {
		if r.ID == id {
			next := append([]Record{}, m.records...)
			next[i].Enabled = enabled
			// 启动失败时保留原配置；停用必须先落盘，避免失败后重启又启用。
			if enabled {
				if err := m.start(next[i]); err != nil {
					m.failures[id] = err.Error()
					return View{}, err
				}
			}
			if err := m.save(next); err != nil {
				if enabled && !r.Enabled {
					m.stop(id)
				}
				return View{}, err
			}
			if !enabled {
				m.stop(id)
			}
			m.records = next
			delete(m.failures, id)
			return m.view(next[i]), nil
		}
	}
	return View{}, errors.New("节点不存在")
}
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range m.records {
		if r.ID == id {
			next := append([]Record{}, m.records[:i]...)
			next = append(next, m.records[i+1:]...)
			if err := m.save(next); err != nil {
				return err
			}
			m.stop(id)
			m.records = next
			delete(m.failures, id)
			return nil
		}
	}
	return errors.New("节点不存在")
}
func (m *Manager) Export(id string) (Export, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.records {
		if r.ID == id {
			if normalizedInboundMode(r.InboundMode) == "http" {
				return Export{}, errors.New("VoCat 上游只支持 SOCKS，请切换 HTTP/SOCKS 混合模式后推送")
			}
			if !alive(m.processes[id]) {
				return Export{}, errors.New("请先启动节点")
			}
			username, password := "", ""
			if r.AuthEnabled {
				username, password = r.Username, r.Password
			}
			return Export{r.ID, "节点 · " + r.Name, net.JoinHostPort("127.0.0.1", fmt.Sprint(r.Port)), username, password, true}, nil
		}
	}
	return Export{}, errors.New("节点不存在")
}
func (m *Manager) save(records []Record) error {
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(m.dir, ".nodes-*")
	if err != nil {
		return errors.New("无法创建节点存储文件")
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(name, filepath.Join(m.dir, "nodes.json"))
	}
	if err != nil {
		return errors.New("保存节点失败，请检查数据目录权限和磁盘空间")
	}
	return nil
}
func availablePort(reserved ...int) (int, error) { return availablePortFor(false, reserved...) }
func availablePortFor(allowLAN bool, reserved ...int) (int, error) {
	return availablePortForUDP(allowLAN, true, reserved...)
}
func availablePortForUDP(allowLAN, udp bool, reserved ...int) (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	start := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return availablePortFromBindingUDP(start, reserved, allowLAN, udp)
}

// 从系统建议的临时端口开始扫描，避免反复分配到已停用节点的预留端口。
func availablePortFrom(start int, reserved []int) (int, error) {
	return availablePortFromBinding(start, reserved, false)
}
func availablePortFromBinding(start int, reserved []int, allowLAN bool) (int, error) {
	return availablePortFromBindingUDP(start, reserved, allowLAN, true)
}
func availablePortFromBindingUDP(start int, reserved []int, allowLAN, udp bool) (int, error) {
	excluded := make(map[int]bool, len(reserved))
	for _, port := range reserved {
		excluded[port] = true
	}
	for offset := 0; offset < 65535; offset++ {
		port := (start-1+offset)%65535 + 1
		if !excluded[port] && checkAvailableBindingUDP(port, allowLAN, udp) == nil {
			return port, nil
		}
	}
	return 0, errors.New("无可用端口")
}

// Config 只设置单一代理出站，不配置 direct 回退。
func Config(r Record) ([]byte, error) {
	n, err := node.Parse(r.Link)
	if err != nil {
		return nil, err
	}
	mode := normalizedInboundMode(r.InboundMode)
	if !validInboundMode(mode) {
		return nil, errors.New("节点入站模式无效")
	}
	protocol := "socks"
	settings := map[string]any{"auth": "noauth", "udp": recordUDPEnabled(r)}
	// LAN 省略 ip，Xray 使用 TCP conn.LocalAddr() 返回可达 UDP 地址。
	if !r.AllowLAN {
		settings["ip"] = "127.0.0.1"
	}
	if r.AuthEnabled {
		if !validCredential(r.Username) || !validCredential(r.Password) {
			return nil, errors.New("SOCKS 用户名和密码必须为 1..255 UTF-8 字节")
		}
		settings["auth"] = "password"
		settings["accounts"] = []any{map[string]string{"user": r.Username, "pass": r.Password}}
	}
	if mode == "http" {
		protocol = "http"
		accounts := settings["accounts"]
		settings = map[string]any{"allowTransparent": false}
		if accounts != nil {
			settings["accounts"] = accounts
		}
	}
	return json.Marshal(map[string]any{
		"log":       map[string]any{"loglevel": "none"},
		"inbounds":  []any{map[string]any{"listen": listenHost(r.AllowLAN), "port": r.Port, "protocol": protocol, "settings": settings}},
		"outbounds": []any{n.Outbound},
	})
}
func (m *Manager) start(r Record) error {
	if alive(m.processes[r.ID]) {
		return nil
	}
	p, err := m.launch(r)
	if err != nil {
		return err
	}
	m.processes[r.ID] = p
	delete(m.failures, r.ID)
	return nil
}

// launch 不改动管理器状态，调用方负责提交或清理候选进程。
func (m *Manager) launch(r Record) (*process, error) {
	if err := m.checkRecordBinding(r); err != nil {
		return nil, err
	}
	data, err := Config(r)
	if err != nil {
		return nil, err
	}
	// 用标准输入传配置，避免在进程参数、临时文件或日志中泄露节点凭据。
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	check := exec.CommandContext(ctx, m.core, "run", "-test", "-config", "stdin:", "-format", "json")
	check.Stdin = strings.NewReader(string(data))
	check.Stdout = io.Discard
	check.Stderr = io.Discard
	if err := check.Run(); err != nil {
		return nil, errors.New("Xray 配置检查失败，请检查内核文件与链接参数")
	}
	cmd := exec.Command(m.core, "run", "-config", "stdin:", "-format", "json")
	cmd.Stdin = strings.NewReader(string(data))
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	configureProcess(cmd)
	p := &process{cmd: cmd, done: make(chan struct{})}
	started := make(chan error, 1)
	go func() {
		// Linux Pdeathsig 跟随创建线程；固定线程直到子进程退出。
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		err := cmd.Start()
		started <- err
		if err == nil {
			_ = cmd.Wait()
		}
		close(p.done)
	}()
	if err := <-started; err != nil {
		return nil, errors.New("无法启动 Xray 内核，请检查执行权限")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !alive(p) {
			return nil, errors.New("Xray 启动后退出，可能端口被占用")
		}
		ready := socksReady
		if normalizedInboundMode(r.InboundMode) == "http" {
			ready = httpReady
		}
		if ready(r) == nil && alive(p) {
			return p, nil
		}
		select {
		case <-p.done:
			return nil, errors.New("Xray 启动后退出，可能端口被占用")
		case <-time.After(60 * time.Millisecond):
		}
	}
	stopProcess(p)
	return nil, errors.New("Xray 本地代理服务未就绪")
}

// httpReady 使用核心本地拒绝响应，不建立任何出站连接。
func httpReady(r Record) error {
	c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", fmt.Sprint(r.Port)), 150*time.Millisecond)
	if err != nil {
		return err
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(200 * time.Millisecond))
	if _, err = io.WriteString(c, "GET / HTTP/1.1\r\nHost: 127.0.0.1\r\nConnection: close\r\n\r\n"); err != nil {
		return err
	}
	response, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	expected := http.StatusBadRequest
	if r.AuthEnabled {
		expected = http.StatusProxyAuthRequired
	}
	if response.StatusCode != expected {
		return errors.New("HTTP 代理就绪响应无效")
	}
	return nil
}
func socksReady(r Record) error {
	c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", fmt.Sprint(r.Port)), 150*time.Millisecond)
	if err != nil {
		return err
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(200 * time.Millisecond))
	method := byte(0)
	if r.AuthEnabled {
		if !validCredential(r.Username) || !validCredential(r.Password) {
			return errors.New("SOCKS 凭据无效")
		}
		method = 2
	}
	if _, err = c.Write([]byte{5, 1, method}); err != nil {
		return err
	}
	b := make([]byte, 2)
	if _, err = io.ReadFull(c, b); err != nil {
		return err
	}
	if b[0] != 5 || b[1] != method {
		return errors.New("SOCKS 认证方式协商失败")
	}
	if r.AuthEnabled {
		packet := append([]byte{1, byte(len(r.Username))}, []byte(r.Username)...)
		packet = append(packet, byte(len(r.Password)))
		packet = append(packet, []byte(r.Password)...)
		if _, err = c.Write(packet); err != nil {
			return err
		}
		if _, err = io.ReadFull(c, b); err != nil {
			return err
		}
		if b[0] != 1 || b[1] != 0 {
			return errors.New("SOCKS 账号密码认证失败")
		}
	}
	return nil
}
func (m *Manager) stop(id string) {
	stopProcess(m.processes[id])
	delete(m.processes, id)
}

func stopProcess(p *process) {
	if alive(p) {
		_ = p.cmd.Process.Kill()
		<-p.done
	}
}
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	for id := range m.processes {
		m.stop(id)
	}
}
