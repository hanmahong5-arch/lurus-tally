#!/usr/bin/env bash
# 用法: scripts/release/cut.sh vX.Y.Z
# 切版本: 校验 -> 测试 -> 生成 CHANGELOG -> 提交 -> 打 tag。本脚本不 push。
set -euo pipefail

VER="${1:-}"
if [[ ! "$VER" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-rc\.[0-9]+)?$ ]]; then
  echo "用法: $0 vX.Y.Z（或 vX.Y.Z-rc.N）" >&2
  exit 2
fi

cd "$(git rev-parse --show-toplevel)"

if [[ -n "$(git status --porcelain)" ]]; then
  echo "工作树不干净，请先提交或清理后再切版本。" >&2
  exit 1
fi

if git rev-parse -q --verify "refs/tags/$VER" >/dev/null; then
  echo "tag $VER 已存在。" >&2
  exit 1
fi

if ! command -v git-cliff >/dev/null 2>&1; then
  echo "未找到 git-cliff。安装: cargo install git-cliff 或 brew install git-cliff（见 https://git-cliff.org）。" >&2
  exit 1
fi

echo "==> go test ./..."
go test ./...

echo "==> web 测试"
(cd web && bun run test)

echo "==> 生成 CHANGELOG.md"
[ -f CHANGELOG.md ] || : > CHANGELOG.md
git cliff --tag "$VER" --unreleased --prepend CHANGELOG.md

git add CHANGELOG.md
git commit -m "chore(release): $VER"
git tag -a "$VER" -m "$VER"

echo
echo "已在本地完成提交与 tag $VER（未 push）。"
echo "请由负责人执行 git push origin main --follow-tags"
