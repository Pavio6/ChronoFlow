# 构建阶段 - 前端
FROM node:22-alpine AS frontend-builder

WORKDIR /app/web

COPY web/package*.json ./
RUN npm ci

COPY web/ ./
RUN npm run build

# 构建阶段 - 后端
FROM golang:1.26.2-alpine AS backend-builder

WORKDIR /app

RUN apk add --no-cache gcc musl-dev

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=frontend-builder /app/web/dist ./web/dist

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o chronoflow ./cmd/chronoflow

# 运行阶段
FROM alpine:latest

WORKDIR /app

RUN apk --no-cache add ca-certificates

COPY --from=backend-builder /app/chronoflow .
COPY --from=backend-builder /app/web/dist ./web/dist
COPY --from=backend-builder /app/config ./config

EXPOSE 8080 8081 8082 8083

ENTRYPOINT ["./chronoflow"]
CMD ["all"]
