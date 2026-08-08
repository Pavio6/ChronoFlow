FROM golang:1.26.2-alpine AS go-builder

WORKDIR /app

RUN apk add --no-cache gcc musl-dev

COPY go.mod go.sum ./
RUN go mod download

COPY . ./

FROM go-builder AS api-builder
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /out/chronoflow-api ./cmd/api

FROM go-builder AS scheduler-builder
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /out/chronoflow-scheduler ./cmd/scheduler

FROM go-builder AS dispatcher-builder
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /out/chronoflow-dispatcher ./cmd/dispatcher

FROM go-builder AS worker-builder
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /out/chronoflow-worker ./cmd/worker

FROM go-builder AS migrate-builder
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /out/chronoflow-migrate ./cmd/migrate

FROM go-builder AS all-builder
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /out/chronoflow-all ./cmd/all

FROM node:22-alpine AS frontend-builder

WORKDIR /app/web

COPY web/package*.json ./
RUN npm ci

COPY web/ ./
RUN npm run build

FROM alpine:3.22 AS runtime-base

WORKDIR /app

RUN apk --no-cache add ca-certificates

COPY --from=go-builder /app/config ./config

FROM runtime-base AS api

COPY --from=api-builder /out/chronoflow-api ./chronoflow-api
COPY --from=frontend-builder /app/web/dist ./web/dist

EXPOSE 8080
ENTRYPOINT ["./chronoflow-api"]

FROM runtime-base AS scheduler

COPY --from=scheduler-builder /out/chronoflow-scheduler ./chronoflow-scheduler

EXPOSE 8081
ENTRYPOINT ["./chronoflow-scheduler"]

FROM runtime-base AS dispatcher

COPY --from=dispatcher-builder /out/chronoflow-dispatcher ./chronoflow-dispatcher

EXPOSE 8082
ENTRYPOINT ["./chronoflow-dispatcher"]

FROM runtime-base AS worker

COPY --from=worker-builder /out/chronoflow-worker ./chronoflow-worker

EXPOSE 8083
ENTRYPOINT ["./chronoflow-worker"]

FROM runtime-base AS migrate

COPY --from=migrate-builder /out/chronoflow-migrate ./chronoflow-migrate
COPY --from=go-builder /app/migrations ./migrations

ENTRYPOINT ["./chronoflow-migrate"]

FROM runtime-base AS all

COPY --from=all-builder /out/chronoflow-all ./chronoflow-all
COPY --from=frontend-builder /app/web/dist ./web/dist

EXPOSE 8080
ENTRYPOINT ["./chronoflow-all"]
