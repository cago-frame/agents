// Package state 给 tool 子包提供跨工具共享的会话状态。
// 目前只有 ReadTracker：read 工具记录"读过哪些文件 + 当时的 mtime/size"，
// edit / write 在动手前来核对 —— 模拟 Claude Code 的 read-before-edit 约束。
package state

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ReadRecord 是一次 read 完成时拍下的文件快照。
type ReadRecord struct {
	Mtime time.Time
	Size  int64
}

// ReadTracker 是线程安全的 path → ReadRecord 表。零值不可用，请用 NewReadTracker。
type ReadTracker struct {
	mu      sync.Mutex
	records map[string]ReadRecord
}

// NewReadTracker 构造一个空 tracker。
func NewReadTracker() *ReadTracker {
	return &ReadTracker{records: make(map[string]ReadRecord)}
}

// Record 在 read 成功后记一笔。path 必须是 *绝对路径*（resolved），否则 Check 不会命中。
func (t *ReadTracker) Record(path string, info os.FileInfo) {
	if t == nil || info == nil {
		return
	}
	abs := canonical(path)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.records[abs] = ReadRecord{Mtime: info.ModTime(), Size: info.Size()}
}

// Forget 把某条记录抹掉（write/edit 写入后内部会调，让下一次再校验需要重新 read）。
func (t *ReadTracker) Forget(path string) {
	if t == nil {
		return
	}
	abs := canonical(path)
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.records, abs)
}

// ErrNotRead 表示 caller 没读过这个文件。LLM 看到这条错误后应该先 read。
var ErrNotRead = errors.New("file has not been read in this session; use read first")

// ErrStale 表示文件自上次 read 之后被外部改了。LLM 看到后应该重新 read。
var ErrStale = errors.New("file has been modified since last read; read it again before editing")

// ErrTrackerNil 表示这个工具没接 tracker。调用方应当忽略 / 不强制约束。
var ErrTrackerNil = errors.New("no read tracker configured")

// Check 校验 path 满足 read-before-edit 约束。
//   - tracker 为 nil → 返回 (nil, ErrTrackerNil)，让 caller 决定是否豁免
//   - 没 read 过 → ErrNotRead
//   - 有 read 过但当前 stat != 记录 → ErrStale
//   - 一致 → 返回 (record, nil)
//
// path 不存在时不报 ErrNotRead 也不报 ErrStale —— write 创建新文件本来就允许。
func (t *ReadTracker) Check(path string) (ReadRecord, error) {
	if t == nil {
		return ReadRecord{}, ErrTrackerNil
	}
	abs := canonical(path)

	st, statErr := os.Stat(abs)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			// 文件不存在：write 新建场景豁免；调用方自己判断要不要走这条路。
			return ReadRecord{}, nil
		}
		return ReadRecord{}, statErr
	}

	t.mu.Lock()
	rec, ok := t.records[abs]
	t.mu.Unlock()
	if !ok {
		return ReadRecord{}, ErrNotRead
	}
	if !rec.Mtime.Equal(st.ModTime()) || rec.Size != st.Size() {
		return rec, ErrStale
	}
	return rec, nil
}

func canonical(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return filepath.Clean(p)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}
