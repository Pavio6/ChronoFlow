package pool

import (
	"context"
	"fmt"
	"time"

	"github.com/panjf2000/ants/v2"
)

// WorkerPool exposes task submission for services and permits lightweight test doubles.
type WorkerPool interface {
	Submit(task func()) error
}

// GoWorkerPool 协程池，封装 ants 库
// 用于限制并发 goroutine 数量，避免资源耗尽
type GoWorkerPool struct {
	pool *ants.Pool
}

// NewGoWorkerPool 创建协程池
// size: 池容量（最大并发数）
// 过期时间默认 1 分钟，空闲协程会被自动回收
func NewGoWorkerPool(size int) (*GoWorkerPool, error) {
	pool, err := ants.NewPool(size, ants.WithExpiryDuration(1*time.Minute))
	if err != nil {
		return nil, fmt.Errorf("创建协程池失败: %w", err)
	}

	return &GoWorkerPool{pool: pool}, nil
}

// Submit 提交任务到协程池
// ants 默认使用阻塞提交语义；池满时等待空闲 worker，而不是丢弃任务。
func (p *GoWorkerPool) Submit(task func()) error {
	if err := p.pool.Submit(task); err != nil {
		return fmt.Errorf("提交任务到协程池失败: %w", err)
	}
	return nil
}

// Release 释放协程池资源
// 调用后不可再提交新任务
func (p *GoWorkerPool) Release() {
	p.pool.Release()
}

// ReleaseTimeout stops accepting work and waits for running workers to exit.
func (p *GoWorkerPool) ReleaseTimeout(timeout time.Duration) error {
	if err := p.pool.ReleaseTimeout(timeout); err != nil {
		return fmt.Errorf("等待协程池停止失败: %w", err)
	}
	return nil
}

// ReleaseContext stops accepting work and shares a caller-owned shutdown deadline.
func (p *GoWorkerPool) ReleaseContext(ctx context.Context) error {
	if err := p.pool.ReleaseContext(ctx); err != nil {
		return fmt.Errorf("等待协程池停止失败: %w", err)
	}
	return nil
}
