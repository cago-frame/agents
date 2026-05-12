// Package filemu 提供按 absolute path 序列化文件读写的 mutex 池。
// edit + write 共用同一个 instance 即可避免同一 Agent 进程内并发改同一文件。
package filemu

import "sync"

// Pool 持有一组按 absolute path 索引的 mutex。零值可用。
type Pool struct {
	mu    sync.Mutex
	locks map[string]*entry
}

type entry struct {
	mu       sync.Mutex
	refcount int
}

// Default 是包级共享池；同一进程内 edit / write 默认共用它。
var Default Pool

// With 在指定 path 上加锁执行 fn。fn 内若再次对同一 path 调 With 会死锁。
func (p *Pool) With(path string, fn func() error) error {
	p.mu.Lock()
	if p.locks == nil {
		p.locks = make(map[string]*entry)
	}
	e, ok := p.locks[path]
	if !ok {
		e = &entry{}
		p.locks[path] = e
	}
	e.refcount++
	p.mu.Unlock()

	e.mu.Lock()
	err := fn()
	e.mu.Unlock()

	p.mu.Lock()
	e.refcount--
	if e.refcount == 0 {
		delete(p.locks, path)
	}
	p.mu.Unlock()
	return err
}
