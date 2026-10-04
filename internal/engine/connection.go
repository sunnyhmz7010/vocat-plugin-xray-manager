package engine

import (
	"errors"
	"fmt"
	"strings"
	"vocat-plugin-xray-manager/internal/node"
)

// Connection 仅在用户打开节点参数时提供原始链接，不进入列表或日志。
func (m *Manager) Connection(id string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.records {
		if r.ID == id {
			return r.Link, nil
		}
	}
	return "", errors.New("节点不存在")
}

// SetConnection 保留本地端口及上游条目 ID，以原配置回滚失败的重启。
func (m *Manager) SetConnection(id, link string) (View, error) {
	link = strings.TrimSpace(link)
	parsed, err := node.Parse(link)
	if err != nil {
		return View{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return View{}, errors.New("插件正在退出")
	}
	for _, r := range m.records {
		if r.ID != id && r.Link == link {
			return View{}, errors.New("此链接已属于另一个节点")
		}
	}
	for i, r := range m.records {
		if r.ID != id {
			continue
		}
		if r.Link == link {
			return m.view(r), nil
		}
		updated := r
		updated.Link, updated.Protocol = link, parsed.Protocol
		next := append([]Record{}, m.records...)
		next[i] = updated
		wasRunning := alive(m.processes[id])
		previousFailure := m.failures[id]
		if wasRunning {
			m.stop(id)
		}
		rollback := func(cause error) (View, error) {
			if wasRunning {
				if restoreErr := m.start(r); restoreErr != nil {
					combined := fmt.Errorf("节点参数保存失败：%w；恢复原服务失败：%v", cause, restoreErr)
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
		var candidate *process
		if wasRunning {
			candidate, err = m.launch(updated)
			if err != nil {
				return rollback(err)
			}
		}
		if err = m.save(next); err != nil {
			stopProcess(candidate)
			return rollback(err)
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
