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

// StreamPublisher publishes durable execution notifications after their MySQL
// Outbox transaction has committed.
type StreamPublisher struct {
	client *goredis.Client
}

// StreamConsumer wraps the Redis Consumer Group operations used by Workers.
type StreamConsumer struct {
	client *goredis.Client
}

func NewStreamConsumer(client *goredis.Client) *StreamConsumer {
	return &StreamConsumer{client: client}
}

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
		return nil, fmt.Errorf("读取 Redis Stream Consumer Group 失败: %w", err)
	}
	return decodeStreams(result)
}

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
		return nil, start, fmt.Errorf("接管 Redis Stream Pending 消息失败: %w", err)
	}
	return decodeMessages(messages), next, nil
}

func (c *StreamConsumer) Ack(
	ctx context.Context,
	stream string,
	group string,
	messageID string,
) error {
	_, err := c.client.XAck(ctx, stream, group, messageID).Result()
	if err != nil {
		return fmt.Errorf("确认 Redis Stream 消息失败: %w", err)
	}
	// XACK is intentionally idempotent. During lease recovery two local
	// handlers can finish around the same time; the first one removes the
	// pending entry and the second one legitimately observes an ACK count of 0.
	return nil
}

func (c *StreamConsumer) PendingCount(
	ctx context.Context,
	stream string,
	group string,
) (int64, error) {
	pending, err := c.client.XPending(ctx, stream, group).Result()
	if err != nil {
		return 0, fmt.Errorf("读取 Redis Stream Pending 状态失败: %w", err)
	}
	return pending.Count, nil
}

// TrimAcknowledgedBefore keeps the oldest pending message and all newer
// entries, so retention never deletes a message still owned by the group.
func (c *StreamConsumer) TrimAcknowledgedBefore(
	ctx context.Context,
	stream string,
	group string,
	cutoff time.Time,
) (int64, error) {
	minID := fmt.Sprintf("%d-0", cutoff.UTC().UnixMilli())
	pending, err := c.client.XPending(ctx, stream, group).Result()
	if err != nil {
		return 0, fmt.Errorf("读取 Stream Pending 边界失败: %w", err)
	}
	if pending.Count > 0 && streamIDLess(pending.Lower, minID) {
		minID = pending.Lower
	}
	trimmed, err := c.client.XTrimMinID(ctx, stream, minID).Result()
	if err != nil {
		return 0, fmt.Errorf("清理已确认 Stream 历史失败: %w", err)
	}
	return trimmed, nil
}

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
			item.DecodeError = fmt.Sprintf("execution_id 无效: %v", err)
		}
		result = append(result, item)
	}
	return result
}

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

func NewStreamPublisher(client *goredis.Client) *StreamPublisher {
	return &StreamPublisher{client: client}
}

func (p *StreamPublisher) EnsureConsumerGroup(
	ctx context.Context,
	stream string,
	group string,
) error {
	err := p.client.XGroupCreateMkStream(ctx, stream, group, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return fmt.Errorf("创建 Redis Stream Consumer Group 失败: %w", err)
	}
	return nil
}

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
		return "", fmt.Errorf("发布 Redis Stream 消息失败: %w", err)
	}
	return messageID, nil
}
