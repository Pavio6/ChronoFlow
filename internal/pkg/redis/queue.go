package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis key 前缀常量
const (
	// TaskQueuePrefix 任务队列 ZSet key 前缀，完整 key: {prefix}{time_range}:{bucket}
	TaskQueuePrefix = "chronoflow:timer:"
	// SchedulerLockPrefix 调度器分布式锁前缀
	SchedulerLockPrefix = "chronoflow:scheduler_lock:"
	// BucketMapPrefix 分桶映射 key 前缀，完整 key: {prefix}{time_range}
	BucketMapPrefix = "chronoflow:bucket:"
	// TaskCountPrefix 分钟累计投递任务数 key 前缀，完整 key: {prefix}{time_range}
	TaskCountPrefix = "chronoflow:task_count:"
)

// TaskTrigger 任务触发信息
type TaskTrigger struct {
	// TimerID 定时器 ID
	TimerID int64
	// TriggerTime 触发时间
	TriggerTime time.Time
}

// RedisQueue 基于 Redis ZSet 的时间片任务队列
type RedisQueue struct {
	client *redis.Client
}

// QueueStats Redis 队列与锁的聚合统计
type QueueStats struct {
	QueueKeys     int64 `json:"queue_keys"`
	QueueItems    int64 `json:"queue_items"`
	LockKeys      int64 `json:"lock_keys"`
	BucketKeys    int64 `json:"bucket_keys"`
	TaskCountKeys int64 `json:"task_count_keys"`
}

// InitRedis 初始化 Redis 客户端连接
func InitRedis(addr, password string, db int) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	// 连接健康检查
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis 连接失败: %w", err)
	}

	return client, nil
}

// NewRedisQueue 创建任务队列实例
func NewRedisQueue(client *redis.Client) *RedisQueue {
	return &RedisQueue{client: client}
}

// buildQueueKey 构建队列 ZSet 的完整 key
// 格式: {TaskQueuePrefix}{time_range}:{bucket}
func buildQueueKey(timeRange string, bucket int) string {
	return fmt.Sprintf("%s%s:%d", TaskQueuePrefix, timeRange, bucket)
}

// buildLockKey 构建分布式锁的完整 key
func buildLockKey(timeRange string, bucket int) string {
	return fmt.Sprintf("%s%s:%d", SchedulerLockPrefix, timeRange, bucket)
}

// PushTask 推送单个任务到指定时间片桶
// 使用 ZAdd 以 triggerTime 的 Unix 时间戳作为 score
func (q *RedisQueue) PushTask(ctx context.Context, timeRange string, bucket int, trigger *TaskTrigger) error {
	key := buildQueueKey(timeRange, bucket)
	member := fmt.Sprintf("%d:%d", trigger.TimerID, trigger.TriggerTime.UnixMilli())
	score := float64(trigger.TriggerTime.Unix())

	return q.client.ZAdd(ctx, key, redis.Z{
		Score:  score,
		Member: member,
	}).Err()
}

// BatchPushTasks 批量推送任务到指定时间片桶（Pipeline 模式）
func (q *RedisQueue) BatchPushTasks(ctx context.Context, timeRange string, bucket int, triggers []*TaskTrigger, expiration time.Duration) error {
	if len(triggers) == 0 {
		return nil
	}

	key := buildQueueKey(timeRange, bucket)
	pipe := q.client.Pipeline()

	for _, trigger := range triggers {
		member := fmt.Sprintf("%d:%d", trigger.TimerID, trigger.TriggerTime.UnixMilli())
		score := float64(trigger.TriggerTime.Unix())
		pipe.ZAdd(ctx, key, redis.Z{
			Score:  score,
			Member: member,
		})
	}
	if expiration > 0 {
		pipe.Expire(ctx, key, expiration)
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("批量推送任务失败: %w", err)
	}

	return nil
}

// GetTasksByTime 获取半开区间 [start, end) 中的任务。
// Trigger 单调推进扫描游标，因此 ZSet 可以保留数据而不在一次处理内重复派发。
func (q *RedisQueue) GetTasksByTime(ctx context.Context, timeRange string, bucket int, start, end time.Time) ([]*TaskTrigger, error) {
	if !start.Before(end) {
		return nil, nil
	}
	key := buildQueueKey(timeRange, bucket)

	members, err := q.client.ZRangeByScore(ctx, key, &redis.ZRangeBy{
		Min: fmt.Sprintf("%d", start.Unix()),
		Max: fmt.Sprintf("(%d", end.Unix()),
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("按时间窗口获取任务失败: %w", err)
	}

	if len(members) == 0 {
		return nil, nil
	}

	triggers := make([]*TaskTrigger, 0, len(members))
	for _, memberStr := range members {
		// 解析 member 格式: {timerID}:{triggerTimeMs}
		var timerID int64
		var triggerTimeMs int64
		if _, err := fmt.Sscanf(memberStr, "%d:%d", &timerID, &triggerTimeMs); err != nil {
			continue
		}

		triggers = append(triggers, &TaskTrigger{
			TimerID:     timerID,
			TriggerTime: time.UnixMilli(triggerTimeMs),
		})
	}

	return triggers, nil
}

// QueueExists 判断指定时间片桶的任务队列是否存在
func (q *RedisQueue) QueueExists(ctx context.Context, timeRange string, bucket int) (bool, error) {
	key := buildQueueKey(timeRange, bucket)
	count, err := q.client.Exists(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("检查任务队列是否存在失败: %w", err)
	}
	return count > 0, nil
}

// buildBucketMapKey 构建分桶映射的完整 key
func buildBucketMapKey(timeRange string) string {
	return fmt.Sprintf("%s%s", BucketMapPrefix, timeRange)
}

func buildTaskCountKey(timeRange string) string {
	return fmt.Sprintf("%s%s", TaskCountPrefix, timeRange)
}

// ReserveMinuteBuckets 为新增任务原子登记分钟级容量，并返回最终可用桶数。
// 桶数只允许增加；已有任务不需要随扩桶重新搬迁。
func (q *RedisQueue) ReserveMinuteBuckets(
	ctx context.Context,
	timeRange string,
	newTaskCount int,
	baseBucketNum int,
	tasksPerBucket int,
	maxBucketNum int,
	expiration time.Duration,
) (int, error) {
	if newTaskCount <= 0 {
		return q.GetBucketNum(ctx, timeRange, baseBucketNum)
	}
	if baseBucketNum < 1 {
		baseBucketNum = 1
	}
	if maxBucketNum < baseBucketNum {
		maxBucketNum = baseBucketNum
	}
	if tasksPerBucket < 1 {
		tasksPerBucket = 1
	}
	ttlMillis := expiration.Milliseconds()
	if ttlMillis < 1 {
		ttlMillis = time.Minute.Milliseconds()
	}

	const reserveScript = `
local total = redis.call('INCRBY', KEYS[2], ARGV[1])
local base = tonumber(ARGV[2])
local perBucket = tonumber(ARGV[3])
local maxBucket = tonumber(ARGV[4])
local ttl = tonumber(ARGV[5])

local required = math.ceil(total / perBucket)
if required < base then required = base end
if required > maxBucket then required = maxBucket end

local current = tonumber(redis.call('GET', KEYS[1]) or '0')
local actual = current
if required > current then
    redis.call('SET', KEYS[1], required)
    actual = required
end
if actual < base then
    redis.call('SET', KEYS[1], base)
    actual = base
end

redis.call('PEXPIRE', KEYS[1], ttl)
redis.call('PEXPIRE', KEYS[2], ttl)
return actual
`

	actual, err := q.client.Eval(ctx, reserveScript, []string{
		buildBucketMapKey(timeRange),
		buildTaskCountKey(timeRange),
	}, newTaskCount, baseBucketNum, tasksPerBucket, maxBucketNum, ttlMillis).Int()
	if err != nil {
		return baseBucketNum, fmt.Errorf("登记分钟动态分桶失败: %w", err)
	}
	return actual, nil
}

// GetBucketNum 获取指定时间范围的分桶数量
// 如果不存在，返回默认值 defaultBucketNum
func (q *RedisQueue) GetBucketNum(ctx context.Context, timeRange string, defaultBucketNum int) (int, error) {
	key := buildBucketMapKey(timeRange)
	val, err := q.client.Get(ctx, key).Int()
	if err == redis.Nil {
		return defaultBucketNum, nil
	}
	if err != nil {
		return defaultBucketNum, fmt.Errorf("获取分桶数量失败: %w", err)
	}
	return val, nil
}

// Stats 汇总尚未到达触发时刻的队列项，以及锁和分桶 key 数量。
// Trigger 为补扫保留已到期成员，因此不能将整个 ZSet 计作待触发任务。
func (q *RedisQueue) Stats(ctx context.Context) (QueueStats, error) {
	var stats QueueStats
	nowExclusive := fmt.Sprintf("(%d", time.Now().Unix())
	if err := q.scanKeys(ctx, TaskQueuePrefix+"*", func(key string) error {
		stats.QueueKeys++
		count, err := q.client.ZCount(ctx, key, nowExclusive, "+inf").Result()
		if err != nil {
			return fmt.Errorf("统计队列长度失败: %w", err)
		}
		stats.QueueItems += count
		return nil
	}); err != nil {
		return stats, err
	}

	if err := q.scanKeys(ctx, SchedulerLockPrefix+"*", func(key string) error {
		stats.LockKeys++
		return nil
	}); err != nil {
		return stats, err
	}

	if err := q.scanKeys(ctx, BucketMapPrefix+"*", func(key string) error {
		stats.BucketKeys++
		return nil
	}); err != nil {
		return stats, err
	}

	if err := q.scanKeys(ctx, TaskCountPrefix+"*", func(key string) error {
		stats.TaskCountKeys++
		return nil
	}); err != nil {
		return stats, err
	}

	return stats, nil
}

func (q *RedisQueue) scanKeys(ctx context.Context, pattern string, visit func(key string) error) error {
	var cursor uint64
	for {
		keys, nextCursor, err := q.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return fmt.Errorf("扫描 Redis key 失败: %w", err)
		}
		for _, key := range keys {
			if err := visit(key); err != nil {
				return err
			}
		}
		if nextCursor == 0 {
			return nil
		}
		cursor = nextCursor
	}
}
