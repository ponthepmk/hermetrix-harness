package product

import (
	"context"
	"runtime"
	"strings"
	"sync"
)

type pathLockManager struct {
	mu      sync.Mutex
	entries map[string]*pathLockEntry
}

type pathLockEntry struct {
	token chan struct{}
	refs  int
}

func newPathLockManager() *pathLockManager {
	return &pathLockManager{entries: map[string]*pathLockEntry{}}
}

func (m *pathLockManager) acquire(ctx context.Context, key string) (func(), error) {
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	m.mu.Lock()
	entry := m.entries[key]
	if entry == nil {
		entry = &pathLockEntry{token: make(chan struct{}, 1)}
		m.entries[key] = entry
	}
	entry.refs++
	m.mu.Unlock()

	select {
	case entry.token <- struct{}{}:
		var once sync.Once
		return func() {
			once.Do(func() {
				<-entry.token
				m.releaseRef(key, entry)
			})
		}, nil
	case <-ctx.Done():
		m.releaseRef(key, entry)
		return nil, ctx.Err()
	}
}

func (m *pathLockManager) releaseRef(key string, entry *pathLockEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry.refs--
	if entry.refs == 0 && m.entries[key] == entry {
		delete(m.entries, key)
	}
}
