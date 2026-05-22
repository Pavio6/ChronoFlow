package bloom

import (
	"context"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"math"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/spaolacci/murmur3"
)

const (
	// MaxBits 布隆过滤器最大位数
	MaxBits = math.MaxInt32
)

// Filter 基于 Redis 的布隆过滤器
// 使用 SHA1 + Murmur3 双哈希策略，每次检查和设置两个位
type Filter struct {
	client *redis.Client
}

// NewFilter 创建布隆过滤器实例
func NewFilter(client *redis.Client) *Filter {
	return &Filter{client: client}
}

// hashSHA1 使用 SHA1 生成哈希值
func hashSHA1(val string) uint32 {
	h := sha1.New()
	h.Write([]byte(val))
	sum := h.Sum(nil)
	// 取前 4 字节转为 uint32
	return binary.BigEndian.Uint32(sum[:4])
}

// hashMurmur3 使用 Murmur3 生成哈希值
func hashMurmur3(val string) uint32 {
	return murmur3.Sum32([]byte(val))
}

// Exist 检查值是否可能存在于布隆过滤器中
// 检查两个哈希位是否都被设置，任一位未设置则一定不存在
func (f *Filter) Exist(ctx context.Context, key, val string) (bool, error) {
	// 计算两个哈希位的位置
	bit1 := hashSHA1(val) % uint32(MaxBits)
	bit2 := hashMurmur3(val) % uint32(MaxBits)

	// 使用 pipeline 批量检查两个位
	pipe := f.client.Pipeline()
	cmd1 := pipe.GetBit(ctx, key, int64(bit1))
	cmd2 := pipe.GetBit(ctx, key, int64(bit2))

	if _, err := pipe.Exec(ctx); err != nil {
		// key 不存在时 GetBit 返回 0，不算错误
		if err != redis.Nil {
			return false, fmt.Errorf("布隆过滤器查询失败: %w", err)
		}
	}

	// 两个位都为 1 时认为可能存在
	return cmd1.Val() == 1 && cmd2.Val() == 1, nil
}

// Set 在布隆过滤器中设置值对应的位
// 使用 pipeline 同时设置两个位，如果是新 key 则设置过期时间
func (f *Filter) Set(ctx context.Context, key, val string, expireSeconds int64) error {
	// 计算两个哈希位的位置
	bit1 := hashSHA1(val) % uint32(MaxBits)
	bit2 := hashMurmur3(val) % uint32(MaxBits)

	// 先检查 key 是否已存在
	exists, err := f.client.Exists(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("检查布隆过滤器 key 失败: %w", err)
	}

	// 使用 pipeline 设置两个位
	pipe := f.client.Pipeline()
	pipe.SetBit(ctx, key, int64(bit1), 1)
	pipe.SetBit(ctx, key, int64(bit2), 1)

	// 新 key 时设置过期时间
	if exists == 0 && expireSeconds > 0 {
		pipe.Expire(ctx, key, time.Duration(expireSeconds)*time.Second)
	}

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("布隆过滤器设置失败: %w", err)
	}

	return nil
}
