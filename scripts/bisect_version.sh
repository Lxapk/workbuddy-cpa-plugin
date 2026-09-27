#!/bin/sh
# bisect_version.sh —— 构建指定 tag 的插件，在真实 CPA 上跑一次请求并报告结果。
#
# 用法: bisect_version.sh <tag> <port>
#
# 为什么需要它：v0.13.12 能正常对话而 v0.13.13 不能，说明中间某个改动引入了回归。
# 单看 diff 无法下定论——改动很多且互相叠加，只有让真实的 CPA 加载这个插件、真的
# 打一次上游才能分辨。
#
# 关键：插件是经 local-registry.json 安装并注册的，不是把 .so 放进目录就会生效。
# 之前失败的做法只替换了 .so，结果 CPA 报告 "loaded 0 file-based clients"，
# 请求以 "unknown provider" 被拒——那是插件根本没被加载，与插件版本无关。
# 所以这里同时写入 local-registry.json 的版本号，并保留 .h 头文件。
set -eu

TAG="$1"
PORT="$2"
WORK="/tmp/bisect"

rm -rf "$WORK"
mkdir -p "$WORK/src"
cd /workspace/workbuddy-cpa-plugin
# 导出该 tag 的源码树而不是 checkout：工作区里还有未提交的改动要保护。
git archive "$TAG" | tar -x -C "$WORK/src"

cd "$WORK/src"
export CGO_ENABLED=1 CC=gcc GOOS=linux GOARCH=arm64
if ! go build -buildmode=c-shared -trimpath -o "$WORK/workbuddy.so" . 2>"$WORK/build.log"; then
  echo "BUILD_FAIL"
  tail -5 "$WORK/build.log"
  exit 0
fi

# 组装独立的运行目录。连同 local-registry.json 一起复制：它是插件被识别的依据。
cp -r /tmp/cpa/run "$WORK/run"
rm -rf "$WORK/run/logs"
mkdir -p "$WORK/run/plugins"
cp "$WORK/workbuddy.so" "$WORK/run/plugins/workbuddy.so"
sed -i "s/^port: .*/port: $PORT/" "$WORK/run/config.yaml"

# 让注册表声明的版本与实际插件一致，避免版本不符导致拒绝加载。
python3 - "$WORK/run/local-registry.json" "$TAG" <<'PY'
import json, sys
path, tag = sys.argv[1], sys.argv[2]
version = tag.lstrip('v')
try:
    doc = json.load(open(path))
except Exception:
    doc = {"schema_version": 2, "plugins": []}
if not doc.get("plugins"):
    doc["plugins"] = [{"id": "workbuddy", "name": "WorkBuddy"}]
doc["plugins"][0]["id"] = "workbuddy"
doc["plugins"][0]["version"] = version
json.dump(doc, open(path, "w"), ensure_ascii=False, indent=2)
PY

# 每次都从干净状态开始：上一轮留下的冷却会让这一轮的结论失真。
python3 - "$WORK/run/auths" <<'PY'
import json, glob, sys
for f in glob.glob(sys.argv[1] + '/codebuddy-*.json'):
    d = json.load(open(f))
    d['disabled'] = False
    for k in ('disabled_until', 'model_states'):
        d.pop(k, None)
    json.dump(d, open(f, 'w'), ensure_ascii=False, indent=2)
PY

/tmp/cpa/cpa-server -config "$WORK/run/config.yaml" > "$WORK/server.log" 2>&1 &
SERVER_PID=$!
trap 'kill $SERVER_PID 2>/dev/null || true' EXIT

i=0
while [ $i -lt 30 ]; do
  if curl -sS -o /dev/null "http://127.0.0.1:$PORT/healthz" 2>/dev/null; then break; fi
  sleep 1
  i=$((i + 1))
done

# 先确认插件真的被加载，否则后面的失败无法归因到插件版本。
if ! grep -aq "plugin loaded" "$WORK/server.log"; then
  echo "NOT_LOADED 插件未被 CPA 加载（与插件版本无关）"
  grep -aiE "plugin|loaded .* clients" "$WORK/server.log" | tail -5
  exit 0
fi

RESULT=$(curl -sS --max-time 45 -X POST "http://127.0.0.1:$PORT/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -d '{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"hi"}],"max_tokens":10}' 2>&1)

echo "$RESULT" | python3 -c '
import sys, json
raw = sys.stdin.read()
try:
    d = json.loads(raw)
except Exception:
    print("UNPARSABLE " + raw[:120].replace("\n", " "))
    raise SystemExit
if "choices" in d:
    print("OK " + (d["choices"][0]["message"].get("content") or "")[:40])
else:
    message = (d.get("error") or {}).get("message", "") or raw
    print("FAIL " + message[:140].replace("\n", " "))
'
