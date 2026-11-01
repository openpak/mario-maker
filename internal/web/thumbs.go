package web

import (
	"container/list"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// thumbCache keeps thumbnails in memory (bounded, least recently used out) and,
// when dir is set, on disk. Keys carry the course's updated time, so a changed
// course misses the cache by itself; stale disk files are overwritten, not swept.
type thumbCache struct {
	dir      string
	maxBytes int

	mu    sync.Mutex
	bytes int
	lru   *list.List // of *thumbItem, front = newest
	idx   map[string]*list.Element
}

type thumbItem struct {
	key string
	b   []byte
}

func newThumbCache(dir string, maxBytes int) *thumbCache {
	if dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	return &thumbCache{dir: dir, maxBytes: maxBytes, lru: list.New(), idx: map[string]*list.Element{}}
}

func (c *thumbCache) get(key string) ([]byte, bool) {
	c.mu.Lock()
	if e, ok := c.idx[key]; ok {
		c.lru.MoveToFront(e)
		b := e.Value.(*thumbItem).b
		c.mu.Unlock()
		return b, true
	}
	c.mu.Unlock()
	if c.dir == "" {
		return nil, false
	}
	b, err := os.ReadFile(filepath.Join(c.dir, key+".jpg"))
	if err != nil || len(b) == 0 {
		return nil, false
	}
	c.put(key, b, false)
	return b, true
}

func (c *thumbCache) put(key string, b []byte, toDisk bool) {
	if toDisk && c.dir != "" {
		tmp := filepath.Join(c.dir, fmt.Sprintf(".%s.%d.tmp", key, os.Getpid()))
		if os.WriteFile(tmp, b, 0o644) == nil {
			if os.Rename(tmp, filepath.Join(c.dir, key+".jpg")) != nil {
				_ = os.Remove(tmp)
			}
		}
	}
	if len(b) > c.maxBytes {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.idx[key]; ok {
		c.bytes -= len(e.Value.(*thumbItem).b)
		c.lru.Remove(e)
	}
	c.idx[key] = c.lru.PushFront(&thumbItem{key, b})
	c.bytes += len(b)
	for c.bytes > c.maxBytes {
		last := c.lru.Back()
		it := last.Value.(*thumbItem)
		c.lru.Remove(last)
		delete(c.idx, it.key)
		c.bytes -= len(it.b)
	}
}
