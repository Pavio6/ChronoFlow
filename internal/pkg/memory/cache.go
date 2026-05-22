package memory

import (
	"sync"
	"time"

	"github.com/chronoflow/internal/model"
)

// cacheEntry 缓存条目，包含定时器定义和过期时间
type cacheEntry struct {
	def      *model.TimerDefinition
	expireAt time.Time
}

// TimerCache 定时器定义的本地内存缓存（二级迁移架构）
// 使用读写锁保证并发安全，支持 TTL 过期和批量操作
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
	defer c.mu.RUnlock()

	entry, exists := c.cache[id]
	if !exists {
		return nil, false
	}

	// 检查是否已过期
	if time.Now().After(entry.expireAt) {
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

	c.cache[id] = &cacheEntry{
		def:      def,
		expireAt: time.Now().Add(ttl),
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

	expireAt := time.Now().Add(ttl)
	for id, def := range entries {
		c.cache[id] = &cacheEntry{
			def:      def,
			expireAt: expireAt,
		}
	}
}

// Size 返回当前缓存条目数（包含已过期但未清理的条目）
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

	now := time.Now()
	removed := 0

	for id, entry := range c.cache {
		if now.After(entry.expireAt) {
			delete(c.cache, id)
			removed++
		}
	}

	return removed
}
