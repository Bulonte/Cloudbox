# CloudBox · 基于智能分类与内容指纹的私有云盘系统

> 毕业设计：轻量化部署的私有云盘，Go + Vue3 全栈，四大创新点（智能分类 / 内容指纹 / 智能搜索 / 可视化）。

## 技术栈

| 层 | 选型 |
|---|---|
| 后端 | Go 1.21+ · Gin · GORM · MySQL 8 · Redis 7 |
| 前端 | Vue 3 · TypeScript · Vite · Element Plus · ECharts |
| 智能 | gojieba 中文分词 · SimHash · pHash · 规则分类器 |
| 部署 | Docker Compose（MySQL + Redis，MinIO 可选） |

## 目录结构

```
cloudbox/
├── backend/    # Go 后端（W2 起）
├── frontend/   # Vue3 前端（W7 起）
├── docker/     # Compose 与部署配置（W1 起）
├── scripts/    # pre-commit 等工程脚本
└── Makefile    # 统一命令入口：make init / make check / ...
```

## 上手

```bash
cd cloudbox
make init   # 安装依赖 + 生成 .env
make dev    # 启动 MySQL + Redis
make check  # 提交前全量检查
```

启用 Git Hook（强烈建议）：

```bash
mkdir -p .githooks
cp scripts/pre-commit.sh .githooks/pre-commit
chmod +x .githooks/pre-commit
git config core.hooksPath .githooks
```

## 文档

详细计划与规范在 `毕设/docs/`（另行管理，不在本仓）：

- 学校正式版计划书（第 1 级权威）
- `docs/推进计划/`：12 周落地排期与进度跟踪
- `docs/规范/`：企业级项目管理规范
- `docs/决策记录/`：ADR 技术决策
