#!/bin/sh
# 安装/升级 WorkBuddy 插件到当前 CPA 工作目录。
#
# 在 CLIProxyAPI 的工作目录里执行（该目录下有 config.yaml）：
#
#   curl -fsSL https://raw.githubusercontent.com/Lxapk/workbuddy-cpa-plugin/main/install.sh | sh
#
# 为什么需要它：插件商店那条路依赖 registry 与 GitHub 查询结果，而 CPA 会把
# release 查询结果缓存一小时（pluginReleaseCacheTTL），期间商店里显示的还是旧
# 版本；registry 里声明的 sha256 又是为 CI 当时产出的字节钉死的，同一 tag 重新
# 构建就再也对不上。这个脚本绕开两者，直接从 release 取产物放到插件目录。
#
# 选项（环境变量）：
#   VERSION   要安装的版本，默认取最新 release
#   ASSET     产物名，默认按当前平台推断
#   DRY_RUN   设为 1 只打印将要执行的动作
set -eu

REPO="Lxapk/workbuddy-cpa-plugin"
PLUGIN_ID="workbuddy"

# ---- 平台 ----------------------------------------------------------------

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux | darwin) ;;
  *)
    echo "不支持的平台: $os（仅支持 linux / darwin）" >&2
    exit 1
    ;;
esac

# 默认按本机推断；可用 GOARCH 覆盖。
#
# 覆盖是必要的：脚本常见于「在 A 机器上准备 B 机器要用的插件」的场景（比如在
# arm64 的开发机上给 x86_64 的服务器装），而本项目的 CI 只发布 linux/amd64。
if [ -n "${GOARCH:-}" ]; then
  goarch="$GOARCH"
else
  arch=$(uname -m)
  case "$arch" in
    x86_64 | amd64) goarch=amd64 ;;
    aarch64 | arm64) goarch=arm64 ;;
    *)
      echo "不支持的架构: $arch（可用 GOARCH=amd64 显式指定）" >&2
      exit 1
      ;;
  esac
fi

# ---- 目标目录 ------------------------------------------------------------

if [ ! -f "config.yaml" ]; then
  echo "当前目录没有 config.yaml —— 请在 CLIProxyAPI 的工作目录里运行这个脚本。" >&2
  exit 1
fi

plugins_dir="plugins"
if [ -f "config.yaml" ]; then
  # 尊重配置里的 plugins-dir；没有就用默认的 plugins/。
  configured=$(awk '/^plugins-dir:/ {print $2}' config.yaml 2>/dev/null | tr -d '"'"'"'' | head -1)
  if [ -n "${configured:-}" ]; then
    plugins_dir="$configured"
  fi
fi
mkdir -p "$plugins_dir"

file="$PLUGIN_ID.so"

# ---- 取产物 --------------------------------------------------------------

if [ -n "${ASSET:-}" ]; then
  asset="$ASSET"
  version="${VERSION:-asseted}"
else
  version="${VERSION:-}"
  if [ -z "$version" ]; then
    echo "查询最新 release ..."
    version=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
      | sed -n 's/.*"tag_name": *"v\?\([^"]*\)".*/\1/p' | head -1)
    if [ -z "$version" ]; then
      echo "无法确定最新版本；可显式指定，例如 VERSION=0.13.40 $0" >&2
      exit 1
    fi
  fi
  asset="${PLUGIN_ID}_${version}_${os}_${goarch}.zip"
fi

url="https://github.com/$REPO/releases/download/v${version}/${asset}"
target="$plugins_dir/$file"

echo "平台      : $os/$goarch"
echo "版本      : $version"
echo "产物      : $asset"
echo "安装到    : $(pwd)/$target"

if [ "${DRY_RUN:-0}" = "1" ]; then
  echo "（DRY_RUN，未执行）"
  exit 0
fi

tmp=$(mktemp -d)
cleanup() { rm -rf "$tmp"; }
trap cleanup EXIT INT TERM

echo "下载 ..."
if ! curl -fsSL -o "$tmp/$asset" "$url"; then
  echo "下载失败: $url" >&2
  echo "请确认该版本已发布且包含 $os/$goarch 产物。" >&2
  exit 1
fi

# 校验：有 checksums.txt 就核对，没有也不拦（旧版本可能没上传）。
if curl -fsSL -o "$tmp/checksums.txt" \
    "https://github.com/$REPO/releases/download/v${version}/checksums.txt" 2>/dev/null; then
  expected=$(awk -v a="$asset" '$2 == a {print $1}' "$tmp/checksums.txt" | head -1)
  if [ -n "$expected" ]; then
    if command -v sha256sum >/dev/null 2>&1; then
      actual=$(sha256sum "$tmp/$asset" | awk '{print $1}')
    else
      actual=$(shasum -a 256 "$tmp/$asset" | awk '{print $1}')
    fi
    if [ "$actual" != "$expected" ]; then
      echo "校验失败：$asset 的 sha256 与 checksums.txt 不一致" >&2
      echo "  期望 $expected" >&2
      echo "  实际 $actual" >&2
      exit 1
    fi
    echo "校验通过"
  fi
fi

# ---- 解包 ----------------------------------------------------------------

# unzip 不是到处都有（精简的容器镜像里常常没有），所以用它优先、python3 兜底。
extract_archive() {
  archive="$1"
  dest="$2"
  mkdir -p "$dest"
  if command -v unzip >/dev/null 2>&1; then
    unzip -q -o "$archive" -d "$dest"
    return $?
  fi
  if command -v python3 >/dev/null 2>&1; then
    python3 - "$archive" "$dest" <<'PY'
import sys, zipfile
with zipfile.ZipFile(sys.argv[1]) as archive:
    archive.extractall(sys.argv[2])
PY
    return $?
  fi
  echo "需要 unzip 或 python3 来解包，请先安装其中之一。" >&2
  return 1
}

if ! extract_archive "$tmp/$asset" "$tmp/extracted"; then
  echo "解包失败: $asset" >&2
  exit 1
fi

built="$tmp/extracted/$PLUGIN_ID.so"
if [ ! -f "$built" ]; then
  echo "归档里没有 $PLUGIN_ID.so，内容为：" >&2
  ls -1 "$tmp/extracted" >&2
  exit 1
fi

# 先落到同目录的临时名再改名，运行中的 CPA 就不会读到半截文件。
cp "$built" "$plugins_dir/.$file.tmp"
mv -f "$plugins_dir/.$file.tmp" "$target"

echo
echo "已安装: $target"
echo "重启 CPA 使新版本生效。"
