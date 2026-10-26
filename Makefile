# CloudBox 项目 Makefile
# 统一封装常用开发、测试、检查、部署命令
# 使用: make help

SHELL := /bin/bash
BACKEND := backend
FRONTEND := frontend
COMPOSE := docker-compose
COMPOSE_FILE := -f docker/docker-compose.yml

.PHONY: help init deps fmt lint test test-backend test-frontend test-all \
        build build-backend build-frontend run dev up down logs \
        db-migrate db-seed clean security cover check

## ---------- 帮助 ----------
help: ## 显示所有可用命令
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-18s\033[0m %s\n", $$1, $$2}'

## ---------- 初始化 ----------
init: ## 初始化项目（安装依赖 + 生成配置）
	@echo "==> 安装后端依赖"
	cd $(BACKEND) && go mod tidy
	@echo "==> 安装前端依赖"
	cd $(FRONTEND) && npm install
	@echo "==> 复制环境变量模板"
	@[ -f .env ] || cp .env.example .env
	@echo "==> 初始化完成，请编辑 .env 填入实际密钥"

deps: ## 拉取 Docker 依赖镜像
	docker compose pull

## ---------- 代码检查 ----------
fmt: ## 格式化代码
	cd $(BACKEND) && gofmt -w .
	cd $(FRONTEND) && npm run format

lint: ## 静态检查
	cd $(BACKEND) && golangci-lint run --timeout=5m
	cd $(FRONTEND) && npm run lint

type-check: ## TypeScript 类型检查
	cd $(FRONTEND) && npm run type-check

vet: ## go vet
	cd $(BACKEND) && go vet ./...

## ---------- 测试 ----------
test: test-backend test-frontend ## 运行所有单元测试

test-backend: ## 后端单元测试
	cd $(BACKEND) && go test -race -cover ./...

test-frontend: ## 前端单元测试
	cd $(FRONTEND) && npm run test:unit

test-all: ## 全部测试（含集成）
	cd $(BACKEND) && go test -race -tags=integration -cover ./...

cover: ## 生成覆盖率报告
	cd $(BACKEND) && go test -coverprofile=coverage.out -covermode=atomic ./... && \
		go tool cover -html=coverage.out -o coverage.html && \
		echo "覆盖率报告: backend/coverage.html"
	cd $(FRONTEND) && npm run test:unit -- --coverage

## ---------- 安全检查 ----------
security: ## 安全扫描（依赖漏洞 + 密钥泄露）
	@echo "==> Go 依赖漏洞扫描"
	cd $(BACKEND) && (command -v govulncheck >/dev/null && govulncheck ./... || echo "跳过：请先安装 govulncheck")
	@echo "==> npm 依赖审计"
	cd $(FRONTEND) && npm audit --audit-level=high
	@echo "==> 密钥泄露扫描"
	@command -v gitleaks >/dev/null && gitleaks detect --no-git --source . || echo "跳过：请先安装 gitleaks"

## ---------- 提交前完整检查 ----------
check: fmt lint type-check vet test build ## 提交前全量检查

## ---------- 构建 ----------
build: build-backend build-frontend ## 构建全部

build-backend: ## 构建后端二进制
	cd $(BACKEND) && go build -trimpath -ldflags="-s -w" -o bin/server ./cmd/server

build-frontend: ## 构建前端
	cd $(FRONTEND) && npm run build

## ---------- 运行 ----------
run: ## 本地运行后端
	cd $(BACKEND) && go run ./cmd/server

dev: ## 启动本地开发环境（Docker 依赖）
	$(COMPOSE) $(COMPOSE_FILE) up -d mysql redis
	@echo "依赖服务已启动"

up: ## 启动全部服务
	$(COMPOSE) $(COMPOSE_FILE) up -d --build

down: ## 停止全部服务
	$(COMPOSE) $(COMPOSE_FILE) down

logs: ## 查看服务日志
	$(COMPOSE) $(COMPOSE_FILE) logs -f --tail=100

## ---------- 数据库 ----------
db-migrate: ## 执行数据库迁移
	cd $(BACKEND) && go run ./cmd/migrate -up

db-seed: ## 灌入种子数据
	cd $(BACKEND) && go run ./cmd/seed

## ---------- 清理 ----------
clean: ## 清理构建产物
	rm -rf $(BACKEND)/bin $(BACKEND)/coverage.* $(FRONTEND)/dist $(FRONTEND)/coverage
	@echo "清理完成"
