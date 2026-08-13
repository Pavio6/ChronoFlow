package redis

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/chronoflow/internal/model"
	goredis "github.com/redis/go-redis/v9"
)

type StreamMessage struct {
	ID          string
	EventID     string
	EventType   string
	ExecutionID int64
	Payload     string
	DecodeError string
}

// StreamPublisher 在 MySQL Outbox 事务提交后发布持久化执行通知
type StreamPublisher struct {
	client *goredis.Client
}

// StreamConsumer 封装 Worker 使用的 Redis Consumer Group 操作
type StreamConsumer struct {
	client *goredis.Client
}

// NewStreamConsumer 创建 Redis Stream 消费者
func NewStreamConsumer(client *goredis.Client) *StreamConsumer {
	return &StreamConsumer{client: client}
}

// ReadNew 从 Consumer Group 读取尚未投递给任一消费者的新消息
func (c *StreamConsumer) ReadNew(
	ctx context.Context,
	stream string,
	group string,
	consumer string,
	count int64,
	block time.Duration,
) ([]StreamMessage, error) {
	result, err := c.client.XReadGroup(ctx, &goredis.XReadGroupArgs{
		Group:    group,
		Consumer: consumer,
		Streams:  []string{stream, ">"},
		Count:    count,
		Block:    block,
	}).Result()
	if err == goredis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read Redis Stream consumer group: %w", err)
	}
	return decodeStreams(result)
}

// AutoClaim 领取空闲时间超过阈值的 Pending 消息
func (c *StreamConsumer) AutoClaim(
	ctx context.Context,
	stream string,
	group string,
	consumer string,
	minIdle time.Duration,
	start string,
	count int64,
) ([]StreamMessage, string, error) {
	messages, next, err := c.client.XAutoClaim(ctx, &goredis.XAutoClaimArgs{
		Stream:   stream,
		Group:    group,
		Consumer: consumer,
		MinIdle:  minIdle,
		Start:    start,
		Count:    count,
	}).Result()
	if err == goredis.Nil {
		return nil, "0-0", nil
	}
	if err != nil {
		return nil, start, fmt.Errorf("claim Redis Stream pending messages: %w", err)
	}
	return decodeMessages(messages), next, nil
}

// Ack 确认一条已经完成处理的 Stream 消息
func (c *StreamConsumer) Ack(
	ctx context.Context,
	stream string,
	group string,
	messageID string,
) error {
	_, err := c.client.XAck(ctx, stream, group, messageID).Result()
	if err != nil {
		return fmt.Errorf("acknowledge Redis Stream message: %w", err)
	}
	// XACK 被设计为幂等操作。Lease 恢复时两个本地处理器可能几乎同时完成；
	// 第一个会移除 Pending 记录，第二个观察到 ACK 数量为 0 仍是合法结果
	return nil
}

// PendingCount 返回 Consumer Group 中尚未确认的消息数量
func (c *StreamConsumer) PendingCount(
	ctx context.Context,
	stream string,
	group string,
) (int64, error) {
	pending, err := c.client.XPending(ctx, stream, group).Result()
	if err != nil {
		return 0, fmt.Errorf("read Redis Stream pending state: %w", err)
	}
	return pending.Count, nil
}

// TrimAcknowledgedBefore 清理指定时间前已确认的消息，同时保留最早的 Pending 消息及其后的全部消息
func (c *StreamConsumer) TrimAcknowledgedBefore(
	ctx context.Context,
	stream string,
	group string,
	cutoff time.Time,
) (int64, error) {
	minID := fmt.Sprintf("%d-0", cutoff.UnixMilli())
	pending, err := c.client.XPending(ctx, stream, group).Result()
	if err != nil {
		return 0, fmt.Errorf("read Redis Stream pending boundary: %w", err)
	}
	if pending.Count > 0 && streamIDLess(pending.Lower, minID) {
		minID = pending.Lower
	}
	trimmed, err := c.client.XTrimMinID(ctx, stream, minID).Result()
	if err != nil {
		return 0, fmt.Errorf("trim acknowledged Redis Stream history: %w", err)
	}
	return trimmed, nil
}

// decodeStreams 将 Redis 返回的多个 Stream 转换为内部消息列表
func decodeStreams(streams []goredis.XStream) ([]StreamMessage, error) {
	total := 0
	for _, stream := range streams {
		total += len(stream.Messages)
	}
	result := make([]StreamMessage, 0, total)
	for _, stream := range streams {
		result = append(result, decodeMessages(stream.Messages)...)
	}
	return result, nil
}

// decodeMessages 将单个 Stream 的消息转换为内部消息列表
func decodeMessages(messages []goredis.XMessage) []StreamMessage {
	result := make([]StreamMessage, 0, len(messages))
	for _, message := range messages {
		executionID, err := streamInt64(message.Values["execution_id"])
		item := StreamMessage{
			ID:          message.ID,
			EventID:     fmt.Sprint(message.Values["event_id"]),
			EventType:   fmt.Sprint(message.Values["event_type"]),
			ExecutionID: executionID,
			Payload:     fmt.Sprint(message.Values["payload"]),
		}
		if err != nil {
			item.DecodeError = fmt.Sprintf("invalid execution_id: %v", err)
		}
		result = append(result, item)
	}
	return result
}

// streamInt64 将 Redis Stream 中的字段值转换为 int64
func streamInt64(value any) (int64, error) {
	switch typed := value.(type) {
	case int64:
		return typed, nil
	case int:
		return int64(typed), nil
	case string:
		return strconv.ParseInt(typed, 10, 64)
	case []byte:
		return strconv.ParseInt(string(typed), 10, 64)
	default:
		return strconv.ParseInt(fmt.Sprint(value), 10, 64)
	}
}

// streamIDLess 比较两个 Redis Stream 消息 ID 的先后顺序
func streamIDLess(left string, right string) bool {
	parse := func(value string) (int64, int64) {
		var milliseconds, sequence int64
		_, _ = fmt.Sscanf(value, "%d-%d", &milliseconds, &sequence)
		return milliseconds, sequence
	}
	leftMS, leftSequence := parse(left)
	rightMS, rightSequence := parse(right)
	return leftMS < rightMS || (leftMS == rightMS && leftSequence < rightSequence)
}

// NewStreamPublisher 创建 Redis Stream 发布者
func NewStreamPublisher(client *goredis.Client) *StreamPublisher {
	return &StreamPublisher{client: client}
}

// EnsureConsumerGroup 确保指定 Stream 上的 Consumer Group 已存在
func (p *StreamPublisher) EnsureConsumerGroup(
	ctx context.Context,
	stream string,
	group string,
) error {
	err := p.client.XGroupCreateMkStream(ctx, stream, group, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return fmt.Errorf("create Redis Stream consumer group: %w", err)
	}
	return nil
}

// Publish 将 Outbox 事件写入 Redis Stream 并返回消息 ID
func (p *StreamPublisher) Publish(
	ctx context.Context,
	stream string,
	maxLen int64,
	event *model.OutboxEvent,
) (string, error) {
	messageID, err := p.client.XAdd(ctx, &goredis.XAddArgs{
		Stream: stream,
		MaxLen: maxLen,
		Approx: true,
		Values: map[string]any{
			"event_id":     event.EventID,
			"execution_id": event.AggregateID,
			"event_type":   event.EventType,
			"payload":      event.Payload,
		},
	}).Result()
	if err != nil {
		return "", fmt.Errorf("publish Redis Stream message: %w", err)
	}
	return messageID, nil
}
