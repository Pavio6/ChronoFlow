package redis

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const (
	releaseLockScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
end
return 0`
	extendLockScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("EXPIRE", KEYS[1], ARGV[2])
end
return 0`
)

// SchedulerLock is a time-slice lock owned by one worker goroutine.
type SchedulerLock struct {
	client *goredis.Client
	key    string
	token  string
}

// NewSchedulerLock creates a lock whose owner identity follows xTimer's
// process-and-goroutine token scheme.
func (q *RedisQueue) NewSchedulerLock(timeRange string, bucket int) *SchedulerLock {
	return &SchedulerLock{
		client: q.client,
		key:    buildLockKey(timeRange, bucket),
		token:  GetProcessAndGoroutineIDStr(),
	}
}

// Lock acquires the lock with an initial TTL.
func (l *SchedulerLock) Lock(ctx context.Context, expiration time.Duration) (bool, error) {
	ok, err := l.client.SetNX(ctx, l.key, l.token, expiration).Result()
	if err != nil {
		return false, fmt.Errorf("获取调度器锁失败: %w", err)
	}
	return ok, nil
}

// Release deletes the lock only while it is still owned by this worker.
func (l *SchedulerLock) Release(ctx context.Context) error {
	released, err := l.client.Eval(ctx, releaseLockScript, []string{l.key}, l.token).Int()
	if err != nil {
		return fmt.Errorf("释放调度器锁失败: %w", err)
	}
	if released != 1 {
		return fmt.Errorf("释放调度器锁失败: 当前协程不再持有锁")
	}
	return nil
}

// Extend updates the TTL only while the lock is still owned by this worker.
func (l *SchedulerLock) Extend(ctx context.Context, expiration time.Duration) error {
	extended, err := l.client.Eval(ctx, extendLockScript, []string{l.key}, l.token, int64(expiration/time.Second)).Int()
	if err != nil {
		return fmt.Errorf("续期调度器锁失败: %w", err)
	}
	if extended != 1 {
		return fmt.Errorf("续期调度器锁失败: 当前协程不再持有锁")
	}
	return nil
}

// GetCurrentGoroutineID extracts the current goroutine id from runtime.Stack,
// matching the ownership token approach used by xTimer.
func GetCurrentGoroutineID() string {
	buf := make([]byte, 128)
	n := runtime.Stack(buf, false)
	fields := strings.Fields(string(buf[:n]))
	if len(fields) < 2 {
		return "unknown"
	}
	return fields[1]
}

// GetProcessAndGoroutineIDStr returns a per-process, per-worker lock token.
func GetProcessAndGoroutineIDStr() string {
	return strconv.Itoa(os.Getpid()) + "_" + GetCurrentGoroutineID()
}
