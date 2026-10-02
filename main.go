package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"vocat-plugin-xray-manager/internal/engine"
)

func main() {
	log.SetFlags(0)
	listen, dir := os.Getenv("VOCAT_PLUGIN_LISTEN"), os.Getenv("VOCAT_PLUGIN_DATA_DIR")
	host, _, err := net.SplitHostPort(listen)
	if err != nil || host != "127.0.0.1" || dir == "" {
		log.Fatal("此程序需由 VoCat 插件管理器启动（仅监听 127.0.0.1）")
	}
	executable, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	core := filepath.Join(filepath.Dir(executable), "xray")
	if runtime.GOOS == "windows" {
		core += ".exe"
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(core, 0750); err != nil {
			log.Fatal("内置 Xray 不存在或无法设置执行权限")
		}
	}
	privateDir, err := privateDataDir(dir)
	if err != nil {
		log.Fatal(err)
	}
	manager, err := engine.Open(privateDir, core)
	if err != nil {
		log.Fatal(err)
	}
	defer manager.Close()
	server := &http.Server{Addr: listen, Handler: handler(manager), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		manager.Close()
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	log.Print("Xray管理已启动")
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Print("插件监听失败")
		return
	}
}

// VoCat 会公开已安装插件目录内的静态文件；私有配置放在同级的未注册目录。
// 卸载保留该目录，重新安装可恢复节点；彻底清除请先在面板删除节点。
func privateDataDir(dir string) (string, error) {
	absolute, err := filepath.Abs(dir)
	if err != nil || filepath.Base(absolute) != "data" || filepath.Base(filepath.Dir(absolute)) != "xray-manager" {
		return "", errors.New("VoCat 插件数据目录布局不符合预期")
	}
	root := filepath.Dir(filepath.Dir(absolute))
	return filepath.Join(root, ".xray-manager-private"), nil
}

func handler(m *engine.Manager) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, map[string]any{"status": "ok"}) })
	mux.HandleFunc("GET /nodes", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, m.List()) })
	mux.HandleFunc("POST /nodes", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Link string `json:"link"`
			engine.Settings
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			fail(w, 400, "请输入一条有效的节点链接")
			return
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			fail(w, 400, "请求必须是单一 JSON 对象")
			return
		}
		if err := validateHostSettings(input.Settings); err != nil {
			fail(w, 400, err.Error())
			return
		}
		view, err := m.AddWithSettings(input.Link, input.Settings)
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		respond(w, 200, view)
	})
	mux.HandleFunc("PUT /nodes/{id}/settings", func(w http.ResponseWriter, r *http.Request) {
		var input engine.Settings
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			fail(w, 400, "连接设置格式无效")
			return
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			fail(w, 400, "请求必须是单一 JSON 对象")
			return
		}
		if err := validateHostSettings(input); err != nil {
			fail(w, 400, err.Error())
			return
		}
		v, err := m.SetSettings(r.PathValue("id"), input)
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		respond(w, 200, v)
	})
	mux.HandleFunc("POST /nodes/{id}/start", func(w http.ResponseWriter, r *http.Request) {
		v, err := m.SetEnabled(r.PathValue("id"), true)
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		respond(w, 200, v)
	})
	mux.HandleFunc("POST /nodes/{id}/stop", func(w http.ResponseWriter, r *http.Request) {
		v, err := m.SetEnabled(r.PathValue("id"), false)
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		respond(w, 200, v)
	})
	mux.HandleFunc("POST /nodes/{id}/export", func(w http.ResponseWriter, r *http.Request) {
		v, err := m.Export(r.PathValue("id"))
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		respond(w, 200, v)
	})
	mux.HandleFunc("DELETE /nodes/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := m.Delete(r.PathValue("id")); err != nil {
			fail(w, 400, err.Error())
			return
		}
		respond(w, 200, map[string]bool{"deleted": true})
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// VoCat 负责用户会话与 CSRF。拒绝绕过宿主的浏览器请求及跨源表单。
		if r.Header.Get("X-VoCat-Plugin-ID") != "xray-manager" {
			fail(w, 403, "请通过 VoCat 插件页面访问")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host != r.Host {
				fail(w, 403, "拒绝跨源请求")
				return
			}
		}
		if r.Method != "GET" && !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
			fail(w, 415, "需要 application/json 请求")
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// VoCat 会裁剪用户名首尾空白，并把八个星号当作保留旧密码的标记。
func validateHostSettings(s engine.Settings) error {
	if !s.AuthEnabled {
		return nil
	}
	if s.Username != strings.TrimSpace(s.Username) {
		return errors.New("账号首尾不能包含空白字符")
	}
	if s.Password != nil && *s.Password == "********" {
		return errors.New("密码不能是八个星号，该值是 VoCat 的密码保留标记")
	}
	return nil
}

func respond(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}
func fail(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": message}})
}
