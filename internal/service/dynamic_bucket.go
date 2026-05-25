package service

import (
	"context"
	"fmt"
	"time"

	"github.com/chronoflow/internal/config"
	"github.com/chronoflow/internal/pkg/redis"
)

// pushMinuteTriggers registers newly created tasks in the minute-level dynamic
// bucket metadata, then writes them using the final bucket count returned by Redis.
func pushMinuteTriggers(
	ctx context.Context,
	queue *redis.RedisQueue,
	cfg *config.SchedulerConfig,
	triggersByMinute map[string][]*redis.TaskTrigger,
) error {
	for timeRange, triggers := range triggersByMinute {
		if len(triggers) == 0 {
			continue
		}

		expiration, err := dynamicBucketExpiration(timeRange, cfg)
		if err != nil {
			return err
		}

		bucketNum, err := queue.ReserveMinuteBuckets(
			ctx,
			timeRange,
			len(triggers),
			cfg.BaseBucketNum,
			cfg.TasksPerBucket,
			cfg.BucketNum,
			expiration,
		)
		if err != nil {
			return fmt.Errorf("登记时间片 %s 的动态分桶失败: %w", timeRange, err)
		}

		triggersByBucket := make(map[int][]*redis.TaskTrigger)
		for _, trigger := range triggers {
			bucket := int(trigger.TimerID) % bucketNum
			triggersByBucket[bucket] = append(triggersByBucket[bucket], trigger)
		}

		for bucket, bucketTriggers := range triggersByBucket {
			if err := queue.BatchPushTasks(ctx, timeRange, bucket, bucketTriggers, expiration); err != nil {
				return fmt.Errorf("推送时间片 %s bucket %d 的任务失败: %w", timeRange, bucket, err)
			}
		}
	}
	return nil
}

// dynamicBucketExpiration preserves future queues until after their execution
// window and retains completed slices for the configured compensation period.
func dynamicBucketExpiration(timeRange string, cfg *config.SchedulerConfig) (time.Duration, error) {
	sliceStart, err := time.ParseInLocation("2006-01-02-15:04", timeRange, time.Local)
	if err != nil {
		return 0, fmt.Errorf("解析时间片失败: %w", err)
	}

	retention := time.Duration(cfg.BucketMetadataTTL) * time.Second
	if retention <= 0 {
		retention = 10 * time.Minute
	}
	expiration := time.Until(sliceStart.Add(time.Minute).Add(retention))
	if expiration < retention {
		expiration = retention
	}
	return expiration, nil
}
