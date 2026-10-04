package engine

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Rename 仅保存显示名称；原链接、ID和进程是独立数据，不修改也不重启。
func (m *Manager) Rename(id, name string) (View, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 255 || !utf8.ValidString(name) || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return View{}, errors.New("节点名称不能为空，最多 255 UTF-8 字节，不能包含控制字符")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return View{}, errors.New("插件正在退出")
	}
	for i, r := range m.records {
		if r.ID != id {
			continue
		}
		if r.Name == name {
			return m.view(r), nil
		}
		next := append([]Record{}, m.records...)
		next[i].Name = name
		if err := m.save(next); err != nil {
			return View{}, err
		}
		m.records = next
		return m.view(next[i]), nil
	}
	return View{}, errors.New("节点不存在")
}
