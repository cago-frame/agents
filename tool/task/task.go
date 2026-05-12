// Package task 把"任务清单"暴露为一组 CRUD 风格的 agent.Tool。
//
// 工具组（共享同一 *Store）：
//
//   - task_create  追加一条或多条任务（server 分配 id，递增 t1 / t2 / …）
//   - task_list    列出全部或按 status 过滤
//   - task_get     按 id 取单条
//   - task_update  按 id 部分更新（status / content / activeForm / priority）
//   - task_delete  按 id 删除，或 clear=true 清空
//
// 跨多个工具共享状态：
//
//	store := task.NewStore()
//	tools := task.NewSuite(task.WithStore(store))
//
// *Store 自身线程安全，可由宿主直接读写（Snapshot / Replace / Clear）。
package task

import (
	"sync"
	"time"
)

// Status 任务状态。
type Status string

// 三档状态。
const (
	StatusPending    Status = "pending"
	StatusInProgress Status = "in_progress"
	StatusCompleted  Status = "completed"
)

// Priority 任务优先级。
type Priority string

// 三档优先级。
const (
	PriorityHigh   Priority = "high"
	PriorityMedium Priority = "medium"
	PriorityLow    Priority = "low"
)

// Item 一条任务。ID 由 Store 分配，宿主直接写 Replace 时可保留 ID。
type Item struct {
	ID         string    `json:"id"`
	Content    string    `json:"content"`
	Status     Status    `json:"status"`
	ActiveForm string    `json:"activeForm,omitempty"`
	Priority   Priority  `json:"priority,omitempty"`
	CreatedAt  time.Time `json:"created_at,omitzero"`
	UpdatedAt  time.Time `json:"updated_at,omitzero"`
}

// Patch 是 UpdatePatch 的入参：nil 表示该字段不动，非 nil 表示用此值覆盖。
//
// 注意：Content 不允许置空（语义保持与 Item.Content required 一致）；ActiveForm / Priority
// 允许通过传入空字符串显式清空。
type Patch struct {
	Status     *Status
	Content    *string
	ActiveForm *string
	Priority   *Priority
}

// Store 持有当前任务状态。线程安全；多个 task_* 工具共享同一个 *Store。
type Store struct {
	mu     sync.Mutex
	items  []Item
	nextID int
}

// NewStore 构造空 Store。
func NewStore() *Store { return &Store{nextID: 1} }

// Snapshot 返回当前列表的拷贝。
func (s *Store) Snapshot() []Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Item, len(s.items))
	copy(out, s.items)
	return out
}

// Len 返回当前任务数。
func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items)
}

// AddItems 追加并分配 id（item.ID 为空时填 t1 / t2 / …，非空则保留）。
// 返回填好 id 与时间戳后的拷贝。
func (s *Store) AddItems(items []Item) []Item {
	if len(items) == 0 {
		return nil
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Item, 0, len(items))
	for _, it := range items {
		if it.ID == "" {
			it.ID = s.allocIDLocked()
		} else {
			s.bumpNextIDLocked(it.ID)
		}
		if it.Status == "" {
			it.Status = StatusPending
		}
		it.CreatedAt = now
		it.UpdatedAt = now
		s.items = append(s.items, it)
		out = append(out, it)
	}
	return out
}

// GetItem 按 id 取单条；找不到返回 (zero, false)。
func (s *Store) GetItem(id string) (Item, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, it := range s.items {
		if it.ID == id {
			return it, true
		}
	}
	return Item{}, false
}

// UpdatePatch 对 id 对应项做部分字段覆盖。找不到 id 时返回 (zero, false)。
func (s *Store) UpdatePatch(id string, p Patch) (Item, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.items {
		if s.items[i].ID != id {
			continue
		}
		if p.Status != nil {
			s.items[i].Status = *p.Status
		}
		if p.Content != nil {
			s.items[i].Content = *p.Content
		}
		if p.ActiveForm != nil {
			s.items[i].ActiveForm = *p.ActiveForm
		}
		if p.Priority != nil {
			s.items[i].Priority = *p.Priority
		}
		s.items[i].UpdatedAt = time.Now()
		return s.items[i], true
	}
	return Item{}, false
}

// DeleteIDs 按 id 批量删除，返回实际删除数。未匹配的 id 静默忽略（调用方自己检 Snapshot 校验）。
func (s *Store) DeleteIDs(ids []string) int {
	if len(ids) == 0 {
		return 0
	}
	idSet := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		idSet[id] = struct{}{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.items[:0]
	removed := 0
	for _, it := range s.items {
		if _, drop := idSet[it.ID]; drop {
			removed++
			continue
		}
		kept = append(kept, it)
	}
	s.items = kept
	return removed
}

// Clear 清空，返回被清掉的数量。
func (s *Store) Clear() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.items)
	s.items = nil
	return n
}

// Replace 直接整体替换。item.ID 为空时分配新 id，非空时保留并更新 nextID。
// 主要给宿主用：从持久化恢复 / 测试初始化。
func (s *Store) Replace(items []Item) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Item, 0, len(items))
	for _, it := range items {
		if it.ID == "" {
			it.ID = s.allocIDLocked()
		} else {
			s.bumpNextIDLocked(it.ID)
		}
		if it.Status == "" {
			it.Status = StatusPending
		}
		if it.CreatedAt.IsZero() {
			it.CreatedAt = now
		}
		it.UpdatedAt = now
		out = append(out, it)
	}
	s.items = out
}

// allocIDLocked 分配一个新 id；调用方必须持有 s.mu。
func (s *Store) allocIDLocked() string {
	if s.nextID < 1 {
		s.nextID = 1
	}
	id := formatID(s.nextID)
	s.nextID++
	return id
}

// bumpNextIDLocked 让 nextID 跳过宿主显式带进来的 id（避免后续 alloc 冲突）。
func (s *Store) bumpNextIDLocked(id string) {
	n, ok := parseID(id)
	if !ok {
		return
	}
	if n >= s.nextID {
		s.nextID = n + 1
	}
}
