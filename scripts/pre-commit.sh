#!/usr/bin/env bash
# ==============================================================================
# CloudBox 提交前检查脚本
#
# 依据: docs/规范/CloudBox企业级项目管理规范.md
#   §4.1.3 提交前本地自检清单
#   §10.5   提交前检查脚本
#
# 启用方式（在仓库根目录执行）:
#   mkdir -p .githooks
#   cp cloudbox/scripts/pre-commit.sh .githooks/pre-commit
#   chmod +x .githooks/pre-commit
#   git config core.hooksPath .githooks
#
# 退出码: 0 = 全部通过 / 1 = 存在未通过项（阻止提交）
# ==============================================================================
set -uo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SRC" || exit 1

FAILED=0
SKIPPED=0

have() { command -v "$1" >/dev/null 2>&1; }

check() {
    local name="$1"
    shift
    printf "  %-40s" "$name"
    if "$@" >/tmp/cloudbox_check.log 2>&1; then
        printf "${GREEN}通过${NC}\n"
    else
        printf "${RED}失败${NC}\n"
        if [ -s /tmp/cloudbox_check.log ]; then
            sed 's/^/      /' /tmp/cloudbox_check.log | head -n 15
        fi
        FAILED=1
    fi
}

skip() {
    printf "  %-40s${YELLOW}跳过${NC} (未安装 %s)\n" "$1" "$2"
    SKIPPED=$((SKIPPED + 1))
}

section() {
    printf "\n${CYAN}=== %s ===${NC}\n" "$1"
}

# ==============================================================================
section "后端检查 (Go)"
# ==============================================================================
if [ -d backend ]; then
    check "gofmt 格式检查" bash -c '[ -z "$(gofmt -l backend/ 2>/dev/null)" ]'
    check "go vet 静态分析"  bash -c 'cd backend && go vet ./...'
    check "go build 编译"    bash -c 'cd backend && go build ./...'
    check "单元测试"          bash -c 'cd backend && go test ./...'

    if have golangci-lint; then
        check "golangci-lint" bash -c 'cd backend && golangci-lint run --timeout=5m'
    else
        skip "golangci-lint" "golangci-lint"
    fi
else
    printf "${YELLOW}  backend/ 目录不存在，跳过后端检查${NC}\n"
fi

# ==============================================================================
section "前端检查 (Vue3 + TypeScript)"
# ==============================================================================
if [ -d frontend ] && [ -f frontend/package.json ]; then
    check "eslint 代码检查"     bash -c 'cd frontend && npm run lint --silent'
    check "TypeScript 类型检查" bash -c 'cd frontend && npm run type-check --silent'

    if grep -q '"test:unit"' frontend/package.json 2>/dev/null; then
        check "前端单元测试" bash -c 'cd frontend && npm run test:unit --silent'
    else
        skip "前端单元测试" "test:unit 脚本"
    fi
else
    printf "${YELLOW}  frontend/ 目录不存在，跳过前端检查${NC}\n"
fi

# ==============================================================================
section "安全检查"
# ==============================================================================
if have gitleaks; then
    check "密钥泄露扫描 (gitleaks)" bash -c 'gitleaks detect --no-git --source . --quiet'
else
    skip "密钥泄露扫描 (gitleaks)" "gitleaks"
fi

if [ -d backend ] && have govulncheck; then
    check "Go 依赖漏洞扫描" bash -c 'cd backend && govulncheck ./...'
else
    skip "Go 依赖漏洞扫描" "govulncheck"
fi

if [ -f frontend/package.json ] && have npm; then
    check "npm 依赖审计 (high+)" bash -c 'cd frontend && npm audit --audit-level=high --silent'
else
    skip "npm 依赖审计" "npm"
fi

# ==============================================================================
section "暂存内容检查"
# ==============================================================================
check "无密钥文件被暂存" bash -c '
    ! git diff --staged --name-only | grep -qiE "(^|/)\.env($|\.)"
'
check "无日志/编译产物被暂存" bash -c '
    ! git diff --staged --name-only | grep -qiE "\.(log|exe|out|zip|tar|gz)$"
'
check "无 IDE 配置被暂存" bash -c '
    ! git diff --staged --name-only | grep -qiE "(^|/)\.idea/|(^|/)\.vscode/"
'

# ==============================================================================
printf "\n%s\n" "────────────────────────────────────────────────────────────"
if [ $FAILED -ne 0 ]; then
    printf "${RED}存在未通过的检查项，禁止提交${NC}\n"
    printf "${RED}   请修复上述问题后重新提交，或使用 git commit --no-verify 临时跳过（需记录原因）${NC}\n"
    exit 1
fi
if [ $SKIPPED -gt 0 ]; then
    printf "${GREEN}全部检查通过${NC}（有 %d 项因缺少工具被跳过）\n" "$SKIPPED"
else
    printf "${GREEN}全部检查通过${NC}\n"
fi
exit 0
