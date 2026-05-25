package memory

import (
	"context"
	"sync"
	"time"

	"github.com/chronoflow/internal/model"
)

// cacheEntry 缓存条目，包含定时器定义和过期时间
type cacheEntry struct {
	def      *model.TimerDefinition
	expireAt time.Time
}

// TimerCache 定时器完整定义（包含状态）的本地内存缓存（二级迁移架构）
// 使用读写锁保证并发安全，支持 TTL 过期、容量淘汰和批量操作
type TimerCache struct {
	mu      sync.RWMutex
	cache   map[int64]*cacheEntry
	maxSize int
}

// NewTimerCache 创建内存缓存实例
// maxSize: 缓存最大条目数
func NewTimerCache(maxSize int) *TimerCache {
	return &TimerCache{
		cache:   make(map[int64]*cacheEntry, maxSize),
		maxSize: maxSize,
	}
}

// Get 获取定时器定义
// 返回值：定时器定义、是否存在（未过期）
func (c *TimerCache) Get(id int64) (*model.TimerDefinition, bool) {
	c.mu.RLock()
	entry, exists := c.cache[id]
	if !exists {
		c.mu.RUnlock()
		return nil, false
	}
	if !time.Now().After(entry.expireAt) {
		c.mu.RUnlock()
		return entry.def, true
	}
	c.mu.RUnlock()

	// 过期数据不再提供读取，并在访问时从 map 中移除。
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, exists = c.cache[id]
	if !exists || time.Now().After(entry.expireAt) {
		delete(c.cache, id)
		return nil, false
	}
	return entry.def, true
}

// Set 设置定时器缓存条目
// id: 定时器 ID
// def: 定时器定义
// ttl: 缓存过期时间
func (c *TimerCache) Set(id int64, def *model.TimerDefinition, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	c.removeExpiredLocked(now)
	c.ensureCapacityLocked(id)
	c.cache[id] = &cacheEntry{
		def:      def,
		expireAt: now.Add(ttl),
	}
}

// Delete 删除指定 ID 的缓存条目
func (c *TimerCache) Delete(id int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.cache, id)
}

// SetBatch 批量设置缓存条目
// entries: 定时器 ID -> 定时器定义的映射
// ttl: 统一的缓存过期时间
func (c *TimerCache) SetBatch(entries map[int64]*model.TimerDefinition, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	c.removeExpiredLocked(now)
	expireAt := now.Add(ttl)
	for id, def := range entries {
		c.ensureCapacityLocked(id)
		c.cache[id] = &cacheEntry{
			def:      def,
			expireAt: expireAt,
		}
	}
}

// Size 返回当前 map 条目数；后台清理周期之间可能短暂包含过期条目。
func (c *TimerCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.cache)
}

// Cleanup 清理已过期的缓存条目
// 返回被清除的条目数量
func (c *TimerCache) Cleanup() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.removeExpiredLocked(time.Now())
}

// StartCleanup 周期清理不再被读取的过期条目，直至 context 被取消。
func (c *TimerCache) StartCleanup(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.Cleanup()
		case <-ctx.Done():
			return
		}
	}
}

func (c *TimerCache) removeExpiredLocked(now time.Time) int {
	removed := 0
	for id, entry := range c.cache {
		if now.After(entry.expireAt) {
			delete(c.cache, id)
			removed++
		}
	}
	return removed
}

func (c *TimerCache) ensureCapacityLocked(id int64) {
	if c.maxSize <= 0 {
		return
	}
	if _, exists := c.cache[id]; exists || len(c.cache) < c.maxSize {
		return
	}

	var (
		evictID int64
		evictAt time.Time
		found   bool
	)
	for existingID, entry := range c.cache {
		if !found || entry.expireAt.Before(evictAt) {
			evictID = existingID
			evictAt = entry.expireAt
			found = true
		}
	}
	if found {
		delete(c.cache, evictID)
	}
}
