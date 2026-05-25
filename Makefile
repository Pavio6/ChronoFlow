.PHONY: dev-start dev-app dev-frontend clean docker-down docker-logs build-frontend build-backend build test-callback

# 启动 Docker 基础依赖与可观测性栈；后端使用 make dev-app 单独启动
dev-start:
	docker compose up -d mysql redis prometheus grafana
	@echo "等待服务就绪..."
	@sleep 5
	@echo "Docker 基础依赖与可观测性服务已启动"
	@echo "  - MySQL: localhost:3306"
	@echo "  - Redis: localhost:6379"
	@echo "  - Prometheus: http://localhost:9090"
	@echo "  - Grafana: http://localhost:3001"
	@echo "Now run: make dev-app (new terminal) and make dev-frontend (new terminal)"

# 启动后端服务
dev-app:
	go run cmd/server/main.go

# 启动前端开发服务器
dev-frontend:
	cd web && npm run dev

clean:
	docker compose down -v

# 停止 Docker 服务
docker-down:
	docker compose down

# 查看 Docker 日志
docker-logs:
	docker compose logs -f


# 构建前端
build-frontend:
	cd web && npm run build

# 构建后端
build-backend:
	go build -o bin/chronoflow cmd/server/main.go

# 构建前端和后端
build: build-frontend build-backend

# 启动测试回调服务
test-callback:
	go run tests/callback-server/main.go
