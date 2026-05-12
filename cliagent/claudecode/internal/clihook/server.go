package clihook

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

// Server Claude Code hook 命令的回调桥梁。监听 Unix domain socket，
// 每条注册的 hook 分配一个 id，CLI 用 `curl --unix-socket` 发 POST 进来。
// 文件权限 0600 + 随机 socket 路径 + 默认 $TMPDIR 提供隔离；不再单独加 token。
type Server struct {
	mu    sync.Mutex
	path  string
	srv   *http.Server
	hooks []Entry
}

// NewServer 构造一个未启动的 Server。
func NewServer() *Server { return &Server{} }

// AddHook 注册 hook，返回分配的 ID。
func (s *Server) AddHook(e Entry) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := fmt.Sprintf("h%d", len(s.hooks))
	e.ID = id
	s.hooks = append(s.hooks, e)
	return id
}

// SnapshotHooks 返回当前 hook 列表的快照。
func (s *Server) SnapshotHooks() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Entry, len(s.hooks))
	copy(out, s.hooks)
	return out
}

// SocketPath 返回当前监听的 UDS 路径，未启动时为空。
func (s *Server) SocketPath() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.path
}

// Start 幂等启动 UDS 监听。
func (s *Server) Start() error {
	s.mu.Lock()
	if s.srv != nil {
		s.mu.Unlock()
		return nil
	}
	// 先拿个独占的临时文件名，再 remove（UDS 要求目标路径不存在）。
	f, err := os.CreateTemp("", "cago-clihook-*.sock")
	if err != nil {
		s.mu.Unlock()
		return err
	}
	path := f.Name()
	_ = f.Close()
	_ = os.Remove(path)

	ln, err := net.Listen("unix", path)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	_ = os.Chmod(path, 0600)

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handle)
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	s.path = path
	s.srv = srv
	s.mu.Unlock()
	go func() { _ = srv.Serve(ln) }()
	return nil
}

// Shutdown 关闭监听并清理 socket 文件。
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	srv := s.srv
	path := s.path
	s.srv = nil
	s.path = ""
	s.mu.Unlock()
	if srv == nil {
		return nil
	}
	err := srv.Shutdown(ctx)
	if path != "" {
		_ = os.Remove(path)
	}
	return err
}

// HookCommand 生成写入 settings.json 的 shell 命令串。
// curl 原生支持 --unix-socket；URL 里的 host 随意（Go http.Server 不校验）。
func (s *Server) HookCommand(id string) string {
	s.mu.Lock()
	path := s.path
	s.mu.Unlock()
	return fmt.Sprintf("curl -sS --unix-socket %s -H 'X-Hook: %s' --data-binary @- http://x/", path, id)
}

// BuildSettingsJSON 生成 Claude Code 可读的 settings JSON（只含 hooks 段），
// 直接丢给 `claude --settings <json>` argv。空 hook 列表返回空串。
func (s *Server) BuildSettingsJSON() (string, error) {
	hooks := s.SnapshotHooks()
	if len(hooks) == 0 {
		return "", nil
	}
	byStage := map[Stage][]map[string]any{}
	for _, e := range hooks {
		entry := map[string]any{
			"hooks": []map[string]any{
				{"type": "command", "command": s.HookCommand(e.ID)},
			},
		}
		if e.Matcher != "" {
			entry["matcher"] = e.Matcher
		}
		byStage[e.Stage] = append(byStage[e.Stage], entry)
	}
	hooksField := map[string]any{}
	for stage, entries := range byStage {
		hooksField[string(stage)] = entries
	}
	data, err := json.Marshal(map[string]any{"hooks": hooksField})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	id := r.Header.Get("X-Hook")
	if id == "" {
		http.Error(w, "missing X-Hook", http.StatusBadRequest)
		return
	}
	var entry *Entry
	for _, e := range s.SnapshotHooks() {
		if e.ID == id {
			ee := e
			entry = &ee
			break
		}
	}
	if entry == nil {
		http.Error(w, "hook not found", http.StatusNotFound)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var in Input
	if len(body) > 0 {
		if err := json.Unmarshal(body, &in); err != nil {
			http.Error(w, fmt.Sprintf("invalid hook payload: %v", err), http.StatusBadRequest)
			return
		}
	}
	in.Raw = body

	out, err := entry.Fn(r.Context(), in)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if out == nil {
		_, _ = w.Write([]byte(`{}`))
		return
	}
	data, err := json.Marshal(out)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(data)
}
