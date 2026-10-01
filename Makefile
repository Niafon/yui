# Yui Companion — сборка и проверки.
# Production storage is embedded SQLite + FTS5 (ADR-037): no DB daemon.

CORE_DIR    := core
WORKERS_DIR := workers
DESKTOP_DIR := desktop
MOBILE_DIR  := mobile
SQLITE_TAGS := sqlite_fts5

.PHONY: help test test-core test-workers test-mobile run run-dev run-workers build build-mobile fmt vet migrate clean db-up-legacy db-down-legacy

help:
	@echo "make test          — тесты ядра и воркеров"
	@echo "make run           — запустить ядро (SQLite создаётся автоматически)"
	@echo "make run-workers   — запустить четыре AI-воркера локально"
	@echo "make build         — собрать ядро и десктоп-сцену"
	@echo "make migrate       — проверить/создать embedded SQLite schema"
	@echo "make db-up-legacy  — поднять старый PostgreSQL backend (не нужен обычно)"

test: test-core test-workers test-mobile

test-core:
	cd $(CORE_DIR) && go vet -tags $(SQLITE_TAGS) ./... && go test -tags $(SQLITE_TAGS) ./... -race -count=1

test-workers:
	cd $(WORKERS_DIR) && python3 -m unittest discover -s tests -v

test-mobile:
	cd $(MOBILE_DIR) && flutter analyze && flutter test

fmt:
	cd $(CORE_DIR) && gofmt -w .

vet:
	cd $(CORE_DIR) && go vet -tags $(SQLITE_TAGS) ./...

run:
	cd $(CORE_DIR) && go run -tags $(SQLITE_TAGS) ./cmd/yui-core -config ../yui.config.json

run-dev:
	cd $(CORE_DIR) && YUI_DB_DRIVER=memory go run -tags $(SQLITE_TAGS) ./cmd/yui-core -config ../yui.config.json

run-workers:
	cd $(WORKERS_DIR) && python3 -m yui_worker --kind embeddings --port 8801 --model Qwen/Qwen3-Embedding-0.6B --device cpu --embedding-dim 256 & \
	cd $(WORKERS_DIR) && python3 -m yui_worker --kind stt        --port 8802 & \
	cd $(WORKERS_DIR) && python3 -m yui_worker --kind tts        --port 8803 & \
	cd $(WORKERS_DIR) && python3 -m yui_worker --kind vision     --port 8804 & \
	wait

build:
	mkdir -p bin
	cd $(CORE_DIR) && go build -tags $(SQLITE_TAGS) -o ../bin/yui-core ./cmd/yui-core
	cd $(DESKTOP_DIR) && npm run build

build-mobile:
	cd $(MOBILE_DIR) && flutter build apk --debug

build-secret:
	mkdir -p bin
	cd $(CORE_DIR) && GOEXPERIMENT=runtimesecret go build -tags '$(SQLITE_TAGS) yuisecret' -o ../bin/yui-core ./cmd/yui-core
	cd $(DESKTOP_DIR) && npm run build

deps:
	cd $(CORE_DIR) && go mod tidy

# Schema upgrades are embedded and transactional. Opening yui-core applies them.
migrate:
	@echo "SQLite migrations are embedded; start yui-core once to apply them."

# Kept only to export/migrate an existing v0.2 PostgreSQL database.
db-up-legacy:
	docker compose -f deploy/docker-compose.postgres-legacy.yml up -d

db-down-legacy:
	docker compose -f deploy/docker-compose.postgres-legacy.yml down

clean:
	rm -rf bin data $(DESKTOP_DIR)/dist
