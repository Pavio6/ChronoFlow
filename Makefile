.PHONY: dev-start demo-data dev-app dev-frontend clean docker-down docker-logs build-frontend build-backend build test-callback

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

# 创建每分钟调用测试服务的演示任务，并通过正式 API 激活入队。
# 运行前需先启动 make dev-app 与 make test-callback。
#demo-data:
#	@docker compose exec -T -e MYSQL_PWD=123456 mysql mysql -uroot chronoflow < migrations/002_demo_data.sql
#	@docker compose exec -T redis redis-cli -n 1 DEL chronoflow:timer:demo-preview:0 chronoflow:timer:demo-preview:1 chronoflow:timer:demo-preview:2 chronoflow:bucket:demo-preview chronoflow:task_count:demo-preview chronoflow:scheduler_lock:demo-preview:1 >/dev/null
#	@ids="$$(docker compose exec -T -e MYSQL_PWD=123456 mysql mysql -N -B -uroot chronoflow -e "SELECT id FROM timer_definitions WHERE app = 'demo-service' AND callback_body LIKE '%demo-data%' AND status = 'INACTIVE'")"; \
#	for id in $$ids; do \
#		curl -fsS -X POST "http://localhost:8080/api/v1/timers/$$id/activate" >/dev/null; \
#	done
#	@echo "演示任务已激活：每分钟真实调用 tests/callback-server 接口"

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
