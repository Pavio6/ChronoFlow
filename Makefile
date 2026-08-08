.PHONY: dev-start dev-app dev-api dev-scheduler dev-dispatcher dev-worker dev-frontend migrate-up migrate-version migrate-down clean docker-down docker-logs build-frontend build-backend build test test-callback

# 启动 Docker 基础依赖与可观测性栈；后端使用 make dev-app 单独启动
dev-start:
	docker compose up -d mysql redis prometheus grafana
	@echo "等待服务就绪..."
	@until docker compose exec -T -e MYSQL_PWD=123456 mysql mysqladmin ping -h localhost -uroot --silent >/dev/null 2>&1; do sleep 2; done
	@echo "Docker 基础依赖与可观测性服务已启动"
	@echo "  - MySQL: localhost:3306"
	@echo "  - Redis: localhost:6379"
	@echo "  - Prometheus: http://localhost:9090"
	@echo "  - Grafana: http://localhost:3001"
	@echo "Now run: make dev-app (new terminal) and make dev-frontend (new terminal)"

# 启动后端服务
dev-app:
	go run ./cmd/all

# 独立角色启动入口；同一主机并行运行时需要为每个角色配置不同的 server.port
dev-api:
	CHRONOFLOW_SERVER_PORT=8080 go run ./cmd/api

dev-scheduler:
	CHRONOFLOW_SERVER_PORT=8081 go run ./cmd/scheduler

dev-dispatcher:
	CHRONOFLOW_SERVER_PORT=8082 go run ./cmd/dispatcher

dev-worker:
	CHRONOFLOW_SERVER_PORT=8083 go run ./cmd/worker

migrate-up:
	go run ./cmd/migrate up

migrate-version:
	go run ./cmd/migrate version

migrate-down:
	@test -n "$(STEPS)" || (echo "Usage: make migrate-down STEPS=1" && exit 2)
	go run ./cmd/migrate down $(STEPS)

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
	mkdir -p bin
	go build -o bin/chronoflow-api ./cmd/api
	go build -o bin/chronoflow-scheduler ./cmd/scheduler
	go build -o bin/chronoflow-dispatcher ./cmd/dispatcher
	go build -o bin/chronoflow-worker ./cmd/worker
	go build -o bin/chronoflow-migrate ./cmd/migrate
	go build -o bin/chronoflow-all ./cmd/all

# 构建前端和后端
build: build-frontend build-backend

test:
	go test -race ./...

# 启动测试回调服务
test-callback:
	@echo "请提供一个可访问的 HTTP 测试回调服务，并在创建 Timer 时填写其 URL"
