.PHONY: help dev-frontend dev-backend dev docker-up docker-down docker-logs build clean test-callback

# 默认目标
help:
	@echo "ChronoFlow - 可用命令:"
	@echo ""
	@echo "  开发命令:"
	@echo "    make dev-frontend   - 启动前端开发服务器 (端口 3000)"
	@echo "    make dev-backend    - 启动后端服务 (端口 8080)"
	@echo "    make dev            - 同时启动前端和后端"
	@echo ""
	@echo "  Docker 命令:"
	@echo "    make docker-up      - 启动 Docker 服务 (MySQL + Redis)"
	@echo "    make docker-down    - 停止 Docker 服务"
	@echo "    make docker-logs    - 查看 Docker 日志"
	@echo "    make docker-all     - 启动所有服务 (包含前端)"
	@echo ""
	@echo "  构建命令:"
	@echo "    make build          - 构建前端和后端"
	@echo "    make build-frontend - 构建前端"
	@echo "    make build-backend  - 构建后端"
	@echo ""
	@echo "  测试命令:"
	@echo "    make test-callback  - 启动测试回调服务 (端口 9090)"
	@echo ""
	@echo "  清理命令:"
	@echo "    make clean          - 清理构建产物"

# 启动前端开发服务器
dev-frontend:
	cd web && npm run dev

# 启动后端服务
dev-backend:
	go run cmd/server/main.go

# 同时启动前端和后端
dev:
	@echo "启动前端开发服务器 (端口 3000)..."
	cd web && npm run dev &
	@echo "启动后端服务 (端口 8080)..."
	go run cmd/server/main.go

# 启动 Docker 服务 (MySQL + Redis)
docker-up:
	docker-compose up -d mysql redis
	@echo "等待 MySQL 和 Redis 就绪..."
	@sleep 5
	@echo "Docker 服务已启动"
	@echo "  - MySQL: localhost:3306"
	@echo "  - Redis: localhost:6379"

# 停止 Docker 服务
docker-down:
	docker-compose down -v

# 查看 Docker 日志
docker-logs:
	docker-compose logs -f

# 启动所有服务 (包含前端)
docker-all:
	docker-compose --profile frontend up -d
	@echo "所有服务已启动"
	@echo "  - 前端: http://localhost:3000"
	@echo "  - 后端: http://localhost:8080"
	@echo "  - MySQL: localhost:3306"
	@echo "  - Redis: localhost:6379"

# 构建前端
build-frontend:
	cd web && npm run build

# 构建后端
build-backend:
	go build -o bin/chronoflow cmd/server/main.go

# 构建前端和后端
build: build-frontend build-backend

# 清理构建产物
clean:
	rm -rf web/dist
	rm -rf bin
	@echo "构建产物已清理"

# 启动测试回调服务
test-callback:
	go run tests/callback-server/main.go
