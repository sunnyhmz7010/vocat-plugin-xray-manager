package engine

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Settings 的 Password=nil 仅在已经启用认证时保留原密码。
type Settings struct {
	InboundMode string  `json:"inbound_mode"`
	UDPEnabled  *bool   `json:"udp_enabled"`
	Port        int     `json:"port"`
	AllowLAN    bool    `json:"allow_lan"`
	AuthEnabled bool    `json:"auth_enabled"`
	Username    string  `json:"username"`
	Password    *string `json:"password"`
}

func validCredential(s string) bool { return len(s) >= 1 && len(s) <= 255 && utf8.ValidString(s) }
func listenHost(allowLAN bool) string {
	if allowLAN {
		return "0.0.0.0"
	}
	return "127.0.0.1"
}
func normalizedInboundMode(mode string) string {
	if mode == "" {
		return "mixed"
	}
	return mode
}
func recordUDPEnabled(r Record) bool {
	return normalizedInboundMode(r.InboundMode) == "mixed" && !r.UDPDisabled
}
func validInboundMode(mode string) bool { return mode == "mixed" || mode == "http" }
func sameSettings(a, b Record) bool {
	return normalizedInboundMode(a.InboundMode) == normalizedInboundMode(b.InboundMode) && a.UDPDisabled == b.UDPDisabled && a.Port == b.Port && a.AllowLAN == b.AllowLAN && a.AuthEnabled == b.AuthEnabled && (!a.AuthEnabled || (a.Username == b.Username && a.Password == b.Password))
}
func applySettings(r Record, s Settings) (Record, error) {
	next := r
	next.InboundMode = normalizedInboundMode(s.InboundMode)
	if !validInboundMode(next.InboundMode) {
		return Record{}, errors.New("入站模式仅支持 mixed（HTTP/SOCKS 混合）或 http；暂不支持纯 SOCKS")
	}
	next.UDPDisabled = s.UDPEnabled != nil && !*s.UDPEnabled
	if s.Port != 0 {
		next.Port = s.Port
	}
	next.AllowLAN, next.AuthEnabled = s.AllowLAN, s.AuthEnabled
	next.Username, next.Password = "", ""
	if s.AuthEnabled {
		next.Username = s.Username
		if s.Password != nil {
			next.Password = *s.Password
		} else if r.AuthEnabled {
			next.Password = r.Password
		}
		if strings.Contains(next.Username, ":") {
			return Record{}, errors.New("代理账号不能包含冒号，HTTP Basic 认证使用冒号分隔账号和密码")
		}
		if !validCredential(next.Username) || !validCredential(next.Password) {
			return Record{}, errors.New("SOCKS 用户名和密码必须为 1..255 UTF-8 字节，首次启用必须提供密码")
		}
	}
	next.PreviousUsernames = append([]string(nil), r.PreviousUsernames...)
	if r.AuthEnabled && (!next.AuthEnabled || r.Username != next.Username) {
		found := false
		for _, old := range next.PreviousUsernames {
			found = found || old == r.Username
		}
		if !found {
			next.PreviousUsernames = append(next.PreviousUsernames, r.Username)
		}
	}
	return next, nil
}

func (m *Manager) SetSettings(id string, settings Settings) (View, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return View{}, errors.New("插件正在退出")
	}
	if settings.Port < 1 || settings.Port > 65535 {
		return View{}, errors.New("本地端口必须为 1..65535")
	}
	for i, r := range m.records {
		if r.ID != id {
			continue
		}
		updated, err := applySettings(r, settings)
		if err != nil {
			return View{}, err
		}
		if sameSettings(r, updated) {
			return m.view(r), nil
		}
		updated.PreviousPorts = append([]int(nil), r.PreviousPorts...)
		if r.Port != updated.Port {
			found := false
			for _, old := range r.PreviousPorts {
				found = found || old == r.Port
			}
			if !found {
				updated.PreviousPorts = append(updated.PreviousPorts, r.Port)
			}
		}
		next := append([]Record{}, m.records...)
		next[i] = updated
		wasRunning := alive(m.processes[id])
		samePort := r.Port == updated.Port
		previousFailure := m.failures[id]
		// 同端口无法并行监听，先停止原进程；失败恢复原记录和实际运行状态。
		if samePort {
			m.stop(id)
		}
		rollback := func(cause error) (View, error) {
			if samePort && wasRunning {
				if restoreErr := m.start(r); restoreErr != nil {
					combined := fmt.Errorf("修改设置失败：%w；恢复旧进程失败：%v", cause, restoreErr)
					m.failures[id] = combined.Error()
					return View{}, combined
				}
			}
			if previousFailure == "" {
				delete(m.failures, id)
			} else {
				m.failures[id] = previousFailure
			}
			return View{}, cause
		}
		if err := m.checkRecordBinding(updated); err != nil {
			return rollback(err)
		}
		var candidate *process
		// 保留实际启停状态：已启用但已崩溃的节点不会因修改设置而意外启动。
		if wasRunning {
			candidate, err = m.launch(updated)
			if err != nil {
				return rollback(err)
			}
		}
		if err := m.save(next); err != nil {
			stopProcess(candidate)
			return rollback(err)
		}
		if !samePort {
			m.stop(id)
		}
		if candidate != nil {
			m.processes[id] = candidate
		}
		m.records = next
		if wasRunning || !r.Enabled {
			delete(m.failures, id)
		}
		return m.view(updated), nil
	}
	return View{}, errors.New("节点不存在")
}
