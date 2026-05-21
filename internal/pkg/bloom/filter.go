package bloom

import (
	"context"
	"crypto/sha1"
	"fmt"
	"math"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/spaolacci/murmur3"
)

const (
	// MaxBits Redis bitmap 最大位数
	MaxBits = math.MaxInt32
)

// Filter 布隆过滤器
// 基于 Redis bitmap 实现，用于任务去重
type Filter struct {
	client *redis.Client
}

// NewFilter 创建布隆过滤器实例
func NewFilter(client *redis.Client) *Filter {
	return &Filter{
		client: client,
	}
}

// Exist 检查元素是否可能存在
// 使用两个 hash 函数（SHA1 和 Murmur3），失效率约为 2 * 10^-7
func (f *Filter) Exist(ctx context.Context, key, val string) (bool, error) {
	// 计算两个 hash 值
	offset1 := f.hashSHA1(val)
	offset2 := f.hashMurmur3(val)

	// 检查第一个位
	exist1, err := f.client.GetBit(ctx, key, int64(offset1)).Result()
	if err != nil {
		return false, fmt.Errorf("bloom filter get bit failed: %w", err)
	}
	if exist1 == 0 {
		return false, nil
	}

	// 检查第二个位
	exist2, err := f.client.GetBit(ctx, key, int64(offset2)).Result()
	if err != nil {
		return false, fmt.Errorf("bloom filter get bit failed: %w", err)
	}

	return exist2 == 1, nil
}

// Set 添加元素到布隆过滤器
func (f *Filter) Set(ctx context.Context, key, val string, expireSeconds int64) error {
	// 计算两个 hash 值
	offset1 := f.hashSHA1(val)
	offset2 := f.hashMurmur3(val)

	// 使用 pipeline 批量设置位
	pipe := f.client.Pipeline()
	pipe.SetBit(ctx, key, int64(offset1), 1)
	pipe.SetBit(ctx, key, int64(offset2), 1)

	// 如果 key 不存在，设置过期时间
	exists, err := f.client.Exists(ctx, key).Result()
	if err == nil && exists == 0 {
		pipe.Expire(ctx, key, time.Duration(expireSeconds)*time.Second)
	}

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("bloom filter set failed: %w", err)
	}

	return nil
}

// hashSHA1 使用 SHA1 计算 hash 值
func (f *Filter) hashSHA1(val string) uint32 {
	h := sha1.New()
	h.Write([]byte(val))
	sum := h.Sum(nil)

	// 取前 4 字节转为 uint32，并对 MaxBits 取模
	return uint32(sum[0])<<24 | uint32(sum[1])<<16 | uint32(sum[2])<<8 | uint32(sum[3])%MaxBits
}

// hashMurmur3 使用 Murmur3 计算 hash 值
func (f *Filter) hashMurmur3(val string) uint32 {
	h := murmur3.New32()
	h.Write([]byte(val))
	return h.Sum32() % MaxBits
}
