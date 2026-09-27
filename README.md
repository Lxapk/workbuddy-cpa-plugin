# workbuddy — CLIProxyAPI 反向代理插件

从 Android 应用 **「AI 聚合网关」v0.1.18**（`dev.aigw.app`）中提取其**反向代理网关**实现，移植为
[CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) (CPA) v7 的**原生动态插件**（`.so`）。

---

## 1. 源应用的反代实现（逆向结论）

应用本体是个 Shell，网关引擎全部在 `V1` 包：

| 源类 | 职责 |
|---|---|
| `V1/o` extends `h2.AbstractC0573l`（NanoHTTPD） | 网关 HTTP 服务器；`e(Session)` 路由、`j(Session)` 鉴权、`k(Session)` 反代主算法 |
| `V1/k` | 网关引擎：provider 注册表、账号池、调用记账 `r()` |
| `V1/s` | `GatewaySettings` |
| `V1/A` | `Route(providerId, model)` |
| `V1/n` | 单个账号条目（`b`=uid, `c`=label, `e`=disabled, `g`=statusMessage） |
| `V1/C` | 手写 chunked SSE 响应器（含 15s keep-alive 心跳） |
| `V1/m` | 流泵（逐帧解析 usage 并记账） |
| `A0.s` | 账号池健康态：`t()` 选号、`p()` 记失败、`q()` 记成功 |

### 对外路由
```
POST /v1/chat/completions   反向代理入口（OpenAI 兼容）
GET  /v1/models            聚合模型目录
GET  /healthz              存活探针（免鉴权）
GET  /authorize            OAuth 回调（免鉴权）
```
所有响应带 CORS：`Access-Control-Allow-Origin: *`、`Access-Control-Allow-Methods: GET,POST,OPTIONS`、`Access-Control-Allow-Headers: *`。
错误信封：`{"error":{"message":...,"type":"api_error","code":...}}`

### 鉴权（`V1/o.j()`）
1. `settings.allowNoKey == true` → 直接放行
2. `settings.apiKey` 为空 → 拒绝
3. 要求 `Bearer ` 前缀（**忽略大小写**）
4. `MessageDigest.isEqual(token, apiKey)` —— **常量时间比较**

### 反代算法（`V1/o.k()`）
1. 鉴权失败 → `401 invalid_api_key`
2. 读 body（支持 chunked；上限 **8 MB**，8 KB buffer）
   - 空体 → `400 请求体为空`
   - 非 JSON → `400 请求体不是合法 JSON`
3. 取 `stream`、`model`
4. **路由**：
   - `model` 含 `/` 且 `indexOf('/') > 0` → 显式 `provider/model`
   - 否则用 `settings.defaultProvider`
   - 再退到第一个可用 provider
   - 都没有 → `400 缺少 model 参数`
5. 未知 provider → `400 未知供应商：xxx`
6. `provider.k(model)` 模型别名映射，**重写 body 的 `model` 字段**
7. 循环最多 `maxRotate` 次：
   - `A0.s.t(providerId, tried)` 选一个未试过且健康的账号
   - `provider.c(auth, refreshSkewSeconds)` 提前刷新凭据
   - `provider.b(auth, body)` 发起上游请求
   - 响应 `>= 400` → `V1.k.c()` 分类记账并**换号重试**
8. 流式 → `V1/m` 流泵 + `V1/C` 手写 chunked SSE；非流式 → 取全文并解析 `usage`
9. 全部失败 → `503 no_healthy_account`；成功 → `A0.s.q()` 清零错误计数

### 失败分类 → 冷却策略（`V1/k.c()`）
| 情形 | 行为 |
|---|---|
| case 0/1/3（auth / rate / quota） | 立即硬冷却 `quotaCooldownMillis`（默认 12h） |
| case 2 | **永久停用**该账号 |
| case 4/5/6（transient） | 累计连续失败，达 `errorThreshold`（默认 3）才停 `errorCooldownMillis`（默认 10min），否则只停 `softCooldownMillis`（默认 60s） |

### `V1/s` GatewaySettings 默认值（插件 1:1 复刻）
```yaml
port: 8790
api_key: ""
allow_no_key: true
expose_lan: true
only_usable_models: false
refresh_skew_seconds: 86400
max_rotate: 3
quota_cooldown_millis: 43200000   # 12h
soft_cooldown_millis: 60000       # 60s
error_threshold: 3
error_cooldown_millis: 600000     # 10min
log_retention_days: 30
default_provider: "trae"
```

---

## 2. 为什么是「插件」而不是照抄 HTTP 服务器

CPA 已经自带 HTTP 服务器、路由、provider 执行器与凭据池。把原应用的 NanoHTTPD 服务器再抄一遍
既冗余又会和 CPA 抢端口。因此插件**只移植源应用真正独有的那部分逻辑**，其余复用宿主能力：

| 源逻辑 | 插件中的落点 | CPA 能力接口 |
|---|---|---|
| `V1/o.j()` Bearer 常量时间校验 | `frontendauth.go` | `FrontendAuthProvider` |
| `V1/o.k()` step 6-8 路由 + 模型改写 | `routing.go` / `intercept_request.go` | `RequestInterceptor` |
| `V1/o.k()` step 9 失败分类 | `intercept_response.go` | `ResponseInterceptor` |
| `V1/o.p()` + `V1/m` SSE 解析 / usage 累计 | `stream.go` | `StreamChunkInterceptor` |
| `V1/o.r()` 调用记账 | `usage.go` / `usage_handler.go` | `UsagePlugin` |
| `A0.s` + `V1/k.c()` 账号池冷却 | `pool.go` | （供上述 hook 共用） |
| `V1/s` + `AppUiState` 状态面板 | `management.go` | `ManagementAPI` |
| **`N1/B` + `V1/k` WorkBuddy 设备码登录** | **`workbuddy_auth.go` / `auth_provider.go`** | **`AuthProvider`** |
| **`a2/b.java:745` 模型目录** | **`model_provider.go` / `workbuddy_client.go`** | **`ModelProvider`** |
| **`a2/b.java:335` 上游对话** | **`executor.go` / `workbuddy_client.go`** | **`ProviderExecutor`** |
| **`V1/o.k` step 6 路由分发** | **`model_router.go`** | **`ModelRouter`** |
| **`a2/b.java:496` 每日签到** | **`checkin.go` / `checkin_client.go` / `checkin_page.go`** | **`ManagementAPI`** |
| **`a2/b.java:406` 剩余额度** | **`quota.go` / `quota_client.go` / `quota_page.go`** | **`QuotaProvider`** |
| **`A0/s.java:585` 账号选用顺序** | **`pool.go`** | （供上述 hook 共用） |

---

## 2.8 账号轮巡 / 区域选择 / 剩余额度（v0.5.0 新增）

三个机制在源 APK 里是**串在一起**的：

```
额度查询 → 写入账号的 credits → 选号时取 credits 最大者
```

### 2.8.1 区域选择（`a2/b.java:284 D()`）

**判定依据是凭据上的 `domain` 字段**，不是运行时探测：

```java
D(cred) {
  d = cred.domain.toLowerCase()
  if (d.endsWith(".workbuddy.ai") || d == "workbuddy.ai")  return "global"
  else if (d.length() > 0)                                  return "cn"
  else                                                      return 配置默认值
}
```

即：**账号登录时用哪个站点，就永久归属哪个区域。**

### base URL 映射（重要 —— 不是一刀切）

| 用途 | global | cn |
|---|---|---|
| 对话 `/v2/chat/completions` | `www.workbuddy.ai` | **`copilot.tencent.com`** |
| 模型列表 `/console/enterprises/personal/models` | `www.workbuddy.ai` | **`copilot.tencent.com`** |
| 签到 `/v2/billing/meter/daily-checkin` | `www.workbuddy.ai` | **`www.codebuddy.cn`** |
| **额度 `/v2/billing/meter/get-user-resource`** | `www.workbuddy.ai` | **`www.codebuddy.cn`** |
| 登录 state/token | — | `copilot.tencent.com` |
| **Origin/Referer 头** | `www.workbuddy.ai` | **`www.codebuddy.cn`** |

> ⚠️ cn 域下**只有对话和模型列表**走 `copilot.tencent.com`，其余走 `codebuddy.cn`。
> 混用会直接失败。

### 2.8.2 剩余额度

```
POST {base}/v2/billing/meter/get-user-resource
body: {PageNumber:1, PageSize:100, ProductCode:"p_tcaca",
       Status:[0,3], PackageEndTimeRangeBegin/End: now ~ now+3185136000000ms}
```

响应（**注意首字母大写的嵌套**）：

```
data.Response.Data.Accounts[] {
    CycleCapacitySize     周期总容量
    CycleCapacityRemain   周期剩余
    CapacityRemain        回退字段（上面两者都 <=0 时用）
}
```

汇总规则（`a2/b.java:432-450`）：

```
sum = Σ( remain > 0 ? remain : 0 )      // 只累加正数
```

错误处理与源应用一致：

| 情况 | 消息 |
|---|---|
| HTTP 非 2xx | `上游 HTTP <code><body>` |
| 缺 `data` | `响应缺少 data` |
| 其他异常 | `查询额度失败` |

### 2.8.3 账号选用顺序（`A0/s.java:585 t()`）

```java
for (d in accounts) {
    if (d.providerId != providerId) continue
    if (exclude.contains(d.uid)) continue          // 本请求已试过的跳过
    if (!d.disabled && d.enabled && now >= d.untilMillis) {
        if (best == null || d.credits > best.credits) best = d   // ★ 额度最大
    }
}
```

**不是轮询（round-robin），而是「剩余额度最多者优先」。**

> **在 CPA 里的现实**：选哪个凭据由 **CPA 的调度器**决定，插件无法插手
> （插件注册的是 executor，CPA 选好 auth 后才把凭据传进来）。
> 因此插件做的是：把额度读出来用于**展示与排序参考**（面板上的
> 「账号选用顺序」表），并以同样的规则维护自己的账号池视图。

### 失败分类 → 冷却（`W1.b` + `V1/k.java:164,168`）

| 类型 | 触发 | 冷却时长 |
|---|---|---|
| `QUOTA` | 额度耗尽 / 限流 | `quota_cooldown_millis`（默认 12h），**并把 credits 归零** |
| `SOFT` | 瞬时失败 | `soft_cooldown_millis`（默认 60s），连续 `error_threshold` 次后转 `error_cooldown_millis` |
| `ERROR` | 鉴权/硬错误 | `quota_cooldown_millis` |

### 使用

管理面板多了 **「AIGW 额度」** 页：

1. **管理密钥** — 与签到页共用（localStorage）
2. **额度概览** — 「已知额度合计」（无数据时显示 `—`，对应 `N1/R0.java:134`）
3. **刷新** — 「立即刷新全部额度」
4. **自动刷新** — 开关 + 间隔（5-1440 分钟）+ 启动时刷新
5. **账号选用顺序** — 按额度从多到少列出，含冷却类型与可用状态

配置：

```yaml
plugins:
  configs:
    workbuddy:
      quota:
        enabled: true              # 打开定时刷新
        interval_minutes: 30       # 每 30 分钟
        refresh_on_start: true     # 启动时先刷一次
```

HTTP 接口：

```bash
KEY="你的 management key"; BASE="http://127.0.0.1:8317"
P="workbuddy"

curl -s "$BASE/v0/management/$P/quota/status"  -H "Authorization: Bearer $KEY"
curl -s -X POST "$BASE/v0/management/$P/quota/refresh" -H "Authorization: Bearer $KEY"
curl -s -X POST "$BASE/v0/management/$P/quota/config" \
  -H "Authorization: Bearer $KEY" -H 'Content-Type: application/json' \
  -d '{"enabled":true,"interval_minutes":30,"refresh_on_start":true}'
```

---

## 2.7 WorkBuddy 每日签到（v0.4.0 新增）

在插件管理面板新增「**AIGW 签到**」页面，支持**手动立即签到**与**每日自动签到**。

### 源 APK 调用链

```
UI「批量签到」(N1/C0290r0.java:148)
  → N1/i1.java:35   engine.h("codebuddy", uid, "checkin", cb)
  → N1/C0284o.java  engine.i(providerId, uid, "checkin", cb)
  → V1/k.java:462   provider.e(account, "checkin", cb)
  → a2/b.java:496   e(account, action, cb)      ← 1375 指令，jadx 反不了，读 smali
```

### 签到接口（smali `a2/b.smali:2643`）

```
POST {checkinBase}/v2/billing/meter/daily-checkin
     body: {}
     checkinBase = domain=="global" ? https://www.workbuddy.ai : https://www.codebuddy.cn
```

> ⚠️ **签到域名与对话域名不同**：cn 域下对话走 `copilot.tencent.com`，
> 而签到走 `www.codebuddy.cn`。两者不能混用。

两个域名都已实测存在（无 token 返回 401）。

### 响应判定（smali 2676-2919 逐行还原）

```
HTTP 非 2xx  →  失败："签到失败（HTTP <status>）：<body>"

HTTP 2xx:
  code = body.code   （缺失/不可解析 → -1）

  code == 0                        →  ✅ "签到成功"
  code != 0 且 body 含 "已签到"
              或 含 "already"      →  ✅ "今日已签到"（幂等，视为成功）
  其它                             →  ❌ message 字段 或 "签到失败（code=N）"
```

与源 APK 完全一致：**"今日已签到"也算成功**，所以重复签到不会报错。

### 手动签到

打开 `http://你的服务器:8317/management.html` → **AIGW 签到**。

页面上有三块：

1. **管理密钥** — 填入 CPA 的 `remote-management.secret-key`，点「保存到浏览器」
2. **自动签到** — 开关 + 每日时间 + 启动补跑
3. **手动签到** — 点「立即为所有账号签到」

> **密钥保存在浏览器本地（localStorage），不会上传到插件或服务器。**
> 这是必要的：CPA 的管理端从 **HTTP 头**鉴权
> （`Authorization: Bearer <key>` 或 `X-Management-Key`，见
> `internal/api/handlers/management/handler.go:276`），而 HTML 表单无法设置请求头。
> 页面因此改用 `fetch()` 携带密钥，密钥始终留在你自己的浏览器里。

填好密钥后：

插件会：

1. 通过 `host.auth.list` 读取 CPA 里所有 `codebuddy` 账号
2. 逐个调用 `daily-checkin`
3. 在页面上列出每个账号的结果（成功 / 已签到 / 失败 + 原因 + code）

> **签到不消耗管理密钥**：签到逻辑本身走的是插件↔宿主的**进程内 RPC**
> （`host.auth.*`）与腾讯 API，只在**页面调用端点**时才需要密钥。

### 自动签到

同一页面配置：

| 项 | 说明 |
|---|---|
| **启用每日自动签到** | 总开关，默认**关闭** |
| **每天 N 时 M 分执行** | 本地时区，默认 09:00 |
| **启动时补跑** | 当天尚未执行过时，插件加载后立即补一次 |

调度由插件内的轻量循环驱动（每分钟探测一次），并用「当天已跑」标记保证
**每天至多执行一次**，不依赖 cron。

### 配置文件写法

```yaml
plugins:
  configs:
    workbuddy:
      # ... 其它配置 ...
      checkin:
        enabled: true          # 打开自动签到
        hour: 9                # 每天 9 点
        minute: 0
        on_start: true         # 启动时补跑
        retry_on_device_fingerprint: true   # 设备指纹失败后 8s 重试一次
```

> 面板上的开关与配置文件是同一份状态：面板保存后会立即生效（启用时自动启动调度器）。

### 关于「设备指纹」重试

源 APK 在签到被上游以 code **9074**（设备指纹未通过）拒绝时，会 `sleep 8s` 后**重试一次**
（`d2/C0482C.java` 的 `catch (w e5) { if (e5.f != 9074) throw e5; Thread.sleep(8000L); ... }`）。
插件保留了这个行为，由 `retry_on_device_fingerprint` 控制，默认开启。

### HTTP 接口（脚本/自动化调用）

```bash
# 查看配置与历史
curl -s http://127.0.0.1:8317/v0/management/workbuddy/checkin/status

# 触发一次手动签到
curl -s -X POST http://127.0.0.1:8317/v0/management/workbuddy/checkin/run

# 修改自动签到配置
curl -s -X POST http://127.0.0.1:8317/v0/management/workbuddy/checkin/config \
  -H 'Content-Type: application/json' \
  -d '{"enabled":true,"hour":9,"minute":0,"on_start":true}'
```

### 排障

| 现象 | 原因 |
|---|---|
| 「没有可签到的 WorkBuddy 账号」 | CPA 的 auth 存储里没有 codebuddy 账号，先去「认证」页登录 |
| 「读取账号失败」 | `host.auth.list` 不可用（宿主版本过旧） |
| 每个账号都失败 `签到失败（HTTP 401）` | WorkBuddy token 失效，重新登录 |
| `签到失败（code=9074）` | 设备指纹被拒；已自动重试一次仍失败时需重新登录该账号 |
| 自动签到没触发 | 确认 `enabled: true`，且插件在配置的**当天该时刻之后**处于运行状态；可用「启动时补跑」兜底 |
| **点保存/签到后页面一片空白** | v0.4.1 修了表单落到 GET-only resource 路由的问题；v0.4.2 起页面不再用 HTML 表单 |
| **`{"error":"missing management key"}`** | **v0.4.2 已在页面上提供密钥输入框**：填入 CPA 的 `remote-management.secret-key`，点「保存到浏览器」即可。密钥只存在本地浏览器 |
| 用 curl 调端点时报 missing management key | 需要带 `-H "Authorization: Bearer <key>"` 或 `-H "X-Management-Key: <key>"` |

### 管理密钥（v0.4.2）

CPA 的管理端点要求请求头带密钥，而页面是从 resource 路由加载的
（该路由**只接受 GET**）。HTML 表单两者都无法满足，所以页面改为：

```
页面上填密钥 → 存 localStorage → 点按钮时用 fetch() 带上 Authorization 头
```

**密钥全程留在浏览器，不经过插件，也不写入配置文件。**

对应的 curl 等价调用：

```bash
KEY="你的 remote-management.secret-key"
BASE="http://127.0.0.1:8317"

curl -s -X POST "$BASE/v0/management/workbuddy/checkin/run" \
  -H "Authorization: Bearer $KEY" | python3 -m json.tool

curl -s "$BASE/v0/management/workbuddy/checkin/status" \
  -H "Authorization: Bearer $KEY" | python3 -m json.tool
```

> 账号池的**轮换重试**由 CPA 自己的 auth 轮换机制承担；插件负责把源应用的**冷却策略**
> （硬冷却 / 软冷却 / 永久停用）准确地喂给响应 hook。

---

## 2.5 WorkBuddy / CodeBuddy 账号登录（v0.2.0 新增）

源应用里 WorkBuddy 的登录方式在 APK 中是**设备码（DEVICE_CODE）**，
而不是 OAuth 回环 —— 这一点决定了它可以在远程服务器上工作：

```java
// jadxout/sources/Y1/b.java
WEBVIEW_CALLBACK=0, DEVICE_CODE=1, SMS_CODE=2, OAUTH_LOOPBACK=3, NONE=4

// jadxout/sources/a2/b.java:61
public final Y1.b f4226c = Y1.b.f3993e;   // f3993e = DEVICE_CODE
```

### 登录流程（全部在插件内完成）

```
① 插件 POST https://copilot.tencent.com/v2/plugin/auth/state?platform=CLI
   ←  {"code":0,"data":{"state":"<uuid>","authUrl":"https://copilot.tencent.com/login?..."}}

② 你把上面这个 authUrl 在浏览器打开并登录

③ 插件轮询 GET https://copilot.tencent.com/v2/plugin/auth/token?state=<state>
   ←  {"code":11217}                       → 继续等（N1/B.java 的 pending 分支）
   ←  {"code":0,"data":{...credentials}}   → 成功
   ←  {"code":<其他>,"msg":"..."}          → 失败

④ 解析凭据 → 通过 host.auth.save 写入 CPA 账号存储
```

**关键点：全程不需要公网回调地址**，因为轮询是插件主动发起的。
这也是它能在腾讯云服务器上跑通、而 OAuth 回环方式（APK 硬编码 `127.0.0.1:51120`）
不行的根本原因。

### 怎么用

插件注册了 `AuthProvider` 后，CPA 管理面板会**自动出现 WorkBuddy 的登录入口**
（面板从 `auth.identifier` 推导出 `<provider>-auth-url` 按钮）。

1. 打开 `http://你的服务器:8317/management.html`
2. 进入 **认证 / Auth**
3. 找到 **WorkBuddy**（provider key 为 `codebuddy`），点登录
4. 按提示在浏览器打开授权链接并登录
5. 页面会自动轮询，登录完成后账号出现在列表里

### 凭据字段（`a2/b.java:t()`）

| 字段 | 别名 |
|---|---|
| `accessToken` | `access_token` |
| `refreshToken` | `refresh_token` |
| `expiresAt` | `expires_at` |
| `uid` | `userId` / `user_id`（缺失时从 JWT claim 回填） |
| `enterpriseId` | `enterprise_id` / `entId` / `tenantId` / `tenant_id`（同上） |
| `nickname` | `nickName` / `name` |

### 域名切换（`a2/b.java:q()`）

| domain | API 基址 | Origin/Referer |
|---|---|---|
| `global` | `https://www.workbuddy.ai` | `https://www.workbuddy.ai` |
| `cn`（默认） | `https://copilot.tencent.com` | `https://www.codebuddy.cn` |

### 手动粘贴凭据

除了交互式登录，管理面板的「上传认证文件」也支持本插件解析：

- 完整的凭据 JSON（即 `a2/b.java:E()` 写出的格式）
- 或直接粘贴一个裸 access token（插件会从 JWT 里解析 `uid` / `tenant_id` / `exp`）

### token 刷新

按 `a2/b.java:c()` 实现：`POST {base}/v2/plugin/auth/token/refresh`，
并用 `expiresIn` 重算 `expiresAt`。

---

## 2.6 把 WorkBuddy 模型以 OpenAI 格式输出（v0.3.0 新增）

这是本插件的**核心用途**：登录 WorkBuddy 后，把它账号下的模型通过 CPA 暴露成
标准 OpenAI 端点，供任意客户端调用。

### 上游接口（`a2/b.java`）

| 用途 | 方法 | 路径 |
|---|---|---|
| 模型目录 | `GET` | `{base}/console/enterprises/personal/models` |
| 对话 | `POST` | `{base_q}/v2/chat/completions` |

`base` / `base_q` 取决于 `domain`：

| domain | chat / models 基址 |
|---|---|
| `global` | `https://www.workbuddy.ai` |
| `cn`（默认） | `https://copilot.tencent.com` |

> **关键优势**：`/v2/chat/completions` 本身就是 **OpenAI Chat Completions 协议**
> （源码里直接读 `stream` / `model` / `tool_choice`），所以**不需要任何格式翻译层** ——
> 客户端请求体只改 `model` 字段后原样透传，响应也原样返回。

### 模型列表过滤规则（`a2/b.java:745 w()`）

```
1. HTTP 必须 2xx，否则报「模型接口 HTTP <code>」
2. 响应必须是合法 JSON，否则报「模型响应不是合法 JSON」
3. code 必须存在且为 0，否则报「模型接口 code=<code>」
4. data 缺失 → 空列表
5. 遍历 data.models[]，逐个保留满足以下全部条件的：
     - id 非空
     - id 未出现过（去重）
     - disabled != true
   name 为空时显示名回退为 id；maxInputTokens 映射为 InputTokenLimit
6. agents 中 name=="cli" 的 models 用于**排序**（cli 列出的排前面），
   不再用作白名单
```

> ⚠️ **v0.8.5 修正**：原实现把 `cli` 列表当作**硬性白名单**，导致上游新增
> 但尚未列入 `cli` 的模型被静默丢弃 —— 例如 `deepseek-v4.1-flash`
> （APK 里只有 `deepseek-v4-flash`）。被丢弃后该模型不在目录中，
> 路由便不认领，最终报 `unknown provider for model`。
> 现在 `cli` 列表只用于排序，**所有上游返回的启用模型都会被保留**。

### 模型名处理（`a2/b.java:583 k()`）

```
model = trim(请求里的 model)
if model == "" || model == "auto"  →  用配置的 default_model
```

插件还会额外剥掉 `codebuddy/` 前缀（对应源网关 `V1/o.k` 的显式供应商形式）。

### 路由决策（`model_router.go`）

CPA 需要知道"哪个请求该交给本插件的 executor"。**不认领会直接报
`unknown provider for model <名字>`**（CPA 查不到内置 provider）。

插件按以下顺序判断：

1. 只处理 `chat-completions` 源格式，其他格式一律不管
2. `codebuddy/<model>` 显式前缀 → 认领（并剥掉前缀）
3. `<其他供应商>/<model>` 显式前缀 → **不认领**，让给对应供应商
4. **本插件有可用的 WorkBuddy 凭据？**
   - 没有 → 不认领（交给宿主）
   - 有 → 认领（命中目录时理由为「在目录中」，否则为「有凭据」）

> **发布的模型名不带前缀。** 模型列表（`/v1/models`、认证文件页、
> `/workbuddy/models`）给出的是**上游裸名**，例如 `deepseek-v4-pro`、
> `kimi-k3`。客户端从列表里复制名字直接请求即可。
>
> 带前缀的写法**依然被接受**（第 2 条会剥掉前缀），所以两种写法都能用，
> 但只有裸名会出现在列表里。

> **第 4 步是 v0.8.4 的关键修正。** 原来只在「模型出现在已缓存目录中」时才认领，
> 而目录是**懒加载**的 —— 若请求先于第一次 `/v1/models` 到达，缓存为空 →
> 不认领 → 报 `unknown provider for model deepseek-v4.1-flash`。
>
> 现在改为以「**本插件是否有该供应商的凭据**」为准（`AvailableProviders` 优先，
> 账号存储兜底）：只要账号存在就认领，真正的非法模型名由上游返回明确错误，
> 这比拒绝路由要好。

**模型目录也会主动拉取**：`model.static` 在缓存为空时会用第一个可用账号
现场查询，而不是返回空列表。

### 配置项

```yaml
plugins:
  configs:
    workbuddy:
      default_model: "auto"    # 空或 auto 时用这个；留空则用目录里第一个
```

> 模型名 **直接使用 WorkBuddy 原生名**，不做重命名 —— 这样客户端看到什么就能调什么。

### 调用示例

```bash
# 先看有哪些模型（登录后自动从 WorkBuddy 拉取）
curl -s http://127.0.0.1:8317/v1/models \
  -H "Authorization: Bearer <你在 CPA 配的 api_key>" | python3 -m json.tool

# 调用（model 填上面列出的原生模型名）
curl -N http://127.0.0.1:8317/v1/chat/completions \
  -H "Authorization: Bearer <你在 CPA 配的 api_key>" \
  -H "Content-Type: application/json" \
  -d '{"model":"<WorkBuddy 的模型名>","stream":true,
       "messages":[{"role":"user","content":"你好"}]}'
```

### 排障

| 现象 | 原因 |
|---|---|
| `/v1/models` 返回 401 | 你没带 CPA 的 api_key。把 `allow_no_key` 设为 `true`，或用 `-H "Authorization: Bearer <api_key>"` |
| `/v1/models` 返回空数组 | 还没登录 WorkBuddy，或登录后模型目录没拉到（看日志；上游需 2xx + `code==0`） |
| 401 `invalid_api_key` | 这是**客户端→CPA** 的鉴权，不是上游问题 |
| 聊天返回上游错误 | 看响应里的 `upstream_status`；401/403 说明 WorkBuddy token 失效，重新登录 |
| 额度刷新报 401/403 | token 失效，重新登录该账号 |
| 额度显示 `—` | 尚未成功查询过任何账号；点「立即刷新全部额度」 |
| 额度为 0 | 上游返回的 `CycleCapacityRemain` 与 `CapacityRemain` 都 <= 0（额度已用尽） |
| 区域显示不对 | 检查凭据里的 `domain` 字段：以 `.workbuddy.ai` 结尾才算 global（`a2/b.java:284`） |
| **`Unexpected JSON token at offset 5: Expected EOF after parsing, but had :`** | **流式 chunk 带了 SSE 的 `data:` 前缀**。CPA 的出站层按裸 JSON 解析每个 chunk，前缀由 CPA 自己加。v0.3.2 已修复，见下方「流式格式」 |
| **`503 auth_not_found: no auth available (providers=codebuddy, ...)`** | **账号没写进 CPA 的 auth 存储**。见下方「账号落盘」。v0.3.1 已修复两个相关缺陷 |

### 流式 chunk 格式（v0.3.2 修复）

`executor.execute_stream` 返回的每个 chunk **必须是裸 JSON**，不能带 SSE 包装：

```
✅ 正确:  {"id":"cmb-...","choices":[{"delta":{"content":"你"}}]}
❌ 错误:  data: {"id":"cmb-...","choices":[...]}\n\n
❌ 错误:  data: [DONE]\n
```

原因：CPA 只在「插件输出格式 ≠ 客户端请求格式」时才跑翻译器
（`internal/pluginhost/adapters_executors.go:552`）。WorkBuddy 两端都是 `chat-completions`，
所以翻译器被跳过，插件返回的 chunk 会**原样**送到响应写出层 —— 而那一层把每个 chunk
当裸 JSON 解析，`data:` 前缀会直接触发 `Unexpected JSON token at offset 5`。

因此插件现在会：

1. 剥掉每帧的 `data:` 前缀（兼容 `data:` 与 `data: ` 两种写法）
2. 丢弃尾部换行
3. 丢弃 `data: [DONE]` 哨兵（**CPA 自己会补**）
4. 丢弃 `: keep-alive` 心跳与空行
5. 丢弃非 JSON 的垃圾帧（避免整条流被一帧坏数据打断）

`data:` 前缀和 SSE 空行由 CPA 负责添加。

### 多轮工具调用被改坏：上游报 tool calls and tool results do not match（v0.13.38 修复）

**现象**：**所有账号**都返回同一个错，而更早的版本（如 v0.13.12）正常。

```
上游未返回任何内容: tool calls and tool results do not match, please start a conversation and retry
```

**为什么和账号无关**：**失败发生在请求体上** —— 换哪个账号都一样，所以
「重试另一个账号」永远救不了它，报成 `auth_unavailable` 只会误导。

**根因**：`repackToolResults`（v0.13.20 引入）把「夹在 `tool_calls` 与其结果
之间」的消息移到结果之后，但它的判断过宽 —— **一个自身带 `tool_calls` 的
assistant 只要后面还有结果，就被当成插入者**。多轮工具调用恰好是这个形状：

```
请求本来合法:  user, call:c1, result:c1, call:c2, result:c2, assistant
被改坏之后:    user, call:c1, result:c1, result:c2, call:c2, assistant
                                   ↑ result:c2 跑到它自己的调用前面了
```

上游于是看到 `result:c2` 找不到对应的调用，整段拒绝。

**修复**：assistant 若自身带 `tool_calls`，它是下一轮交互的开始，批次到此为止，
不是插入者。

**同时修掉一个孪生缺陷**（v0.13.41）：`linkToolResultsToCalls` 会把「名字指向
另一个批次的结果」改写成本批次下一个未回答的调用，使一个调用拿到两个结果、另一个
一个都没有 —— 靠改写制造出的错配。现在只在 id 为**空**或**指向不存在的调用**
时才补/改，顺序不对的结果保持原样。

**排查手法**：这类失败只靠代码 diff 很难定位，因为差异多且互相叠加。用
`scripts/bisect_version.sh <tag> <port>` 把某个 tag 单独编译进一份干净的运行目录
打一次真实请求，就能把「哪个版本开始坏」钉死。



> **注**：这一节记录的是当时的判断，**已被 v0.13.38 回滚**。实测表明宿主读得到
> `metadata` 里的状态码也能自己解释响应体，把它换成错误信封反而夺走了宿主换号、
> 判冷却所需的信息。v0.13.12 一直是以成功信封原样交出上游响应，那正是可用的
> 行为。**结论：不要用这一节的改法**，保留它只为说明排查过程。

**这是「模型限流被报成 `auth_unavailable`」的真正根因。**

#### 缺陷：`status >= 400` 时仍返回成功信封

```go
if status >= 400 {
    reportExecutorFailure(...)           // 插件内部冷却 ✅
}
return okEnvelope(pluginapi.ExecutorResponse{
    Payload:  respBody,                  // ← 错误 JSON 被当正常响应
    Metadata: {"upstream_status": status} // ← CPA 不读 metadata
})                                        // ← ★ 返回「成功」
```

**后果链**：

| 步骤 | 结果 |
|---|---|
| 上游回 429 限流 | — |
| 插件内部冷却 | ✅ |
| **返回成功信封** | **CPA 以为请求成功** ❌ |
| **CPA 看不到 429** | **不重试、不换号** ❌ |
| CPA 记为失败 | **`auth_unavailable`** ❌ |

**CPA 的 `isCredentialRetryRoundStatus` 认 429/502** ——
**但它必须真的收到那个状态码。** 把状态码塞进 `metadata` 等于没给。

**修复**：`status >= 400` 时返回 `errorEnvelope(..., status)`，
**让上游的真实状态码穿过 RPC 边界**；同时新增 `upstreamErrorText()`
从上游 body 提取人类可读文案。

#### 同时修复：折叠路径状态码写死 502

折叠路径用 `statusCodeForFrameFailure(text)` 按文案推断状态码（限流 429、额度 402），
**而不是统一 502**。

#### 停用有害的账号级冷却

曾尝试「限流时把账号也短暂移出轮换」，**实测证明它有害**：

- **CPA 用自己的内存状态回写 auth 文件** → 写入被撤销
- **撤销与重写互相追赶** → debug 日志里 `disabled` 在相邻文件事件间
  `false→true→false` 反复横跳
- **每次翻转重新注册凭据** → 账号在注册表里进进出出
- **被禁用的账号会被 CPA 整体丢弃** → 一个模型限流连累该账号所有模型

`applyAccountPark` / `publishModelPark` 改为 no-op，逻辑保留在 `*Legacy`。

#### 附：文案说明「没有其它账号能接管」

响应拦截里新增 `modelServabilityNote`：当**没有其它可用账号能服务该模型**时，
在错误里补充「请等待重置或改用其他模型」——**因为这时换号本来就没用**。

**背景**：两个 realm 的模型目录不同
（实测：`ai` 注册 18 个模型，`cn` 注册 30 个），
所以**某些模型天然只有一个账号支持**。这**不是故障**，但**报错应该说清**。

### 折叠路径把上游错误折进了回答（v0.13.35 修复）



**排查依据**：插件自带的 `scheduler.pick` 从未被 CPA 调用（日志实测：失败请求
27ms 本地拒绝，无任何插件回调），于是把注意力放回插件自己的执行路径，发现
**非流式折叠路径**有两个缺陷。

#### 缺陷 1：折叠时不检查错误帧

上游把限流写在 **HTTP 200 的流里**。折叠路径只收集 payload：

```go
if payload, keep := sseFrameToBareJSON(frame); keep {
    frames = append(frames, payload)      // 错误帧被当普通帧收集
}
return nil                                 // 从不返回错误
```

**后果**：**限流文本被折叠成「模型的回答」**，而请求**报告成功**。
CPA 拿到一个「成功但内容是错误信息」的响应 —— 既无法分类，也无法冷却。

**修复**：折叠路径与流式路径一致，遇到错误帧立即上报。

#### 缺陷 2：错误帧的状态码写死 502

```go
reportExecutorFailure(creds, ..., http.StatusBadGateway, []byte(errStream.Error()))
//                                    ↑ 限流也被当成 502
```

**502 丢掉了唯一可用的区分依据** —— **429 才是 CPA 认的限流信号**。

**修复**：`statusCodeForFrameFailure` 按**文案**推断状态码：

| 文案 | 状态码 |
|---|---|
| 频率限制 / rate limit | **429** |
| 积分不足 / 额度耗尽 | **402** |
| 认证失败 | **401** |
| 其它 | 502 |

#### 缺陷 3：额度过期短语缺失

`quotaPhrases` 缺 `积分不足` / `积分用完` / `额度用尽` / `payment required` 等 ——
**与源 APK 的短语表（`a2/b.java:48 f4222e`）对齐后补齐**。

#### 顺带：账号级短暂冷却（默认关闭）

**已实现但默认不启用**：限流时把账号也短暂移出轮换，让 CPA 换到健康账号。
**实测发现**：**CPA 会用自己的内存状态回写 auth 文件**，插件写入的
`disabled` / `model_states` 都会被覆盖，**该通道在当前 CPA 版本下不可用**。
代码保留（`model_states.go` / `park_recovery.go`），等 CPA 提供正式接口再启用。

### 错误帧漏认：线索被指向错误的方向（v0.13.34 修复）



**用户的报错**：

```
last upstream error: 上游未返回任何内容: tool calls and tool results do not match,
                     please start a new conversation and retry
```

**注意那个前缀**。它是 `emitted == 0` 时加的 —— 意思是
**「一个 chunk 都没转发出去，流就结束了」**。

**但后半句明明是上游给的 tool 错误** —— **说明上游确实发了错误，只是没被认出来**。

#### 根因：`extractStreamError` 只认一种形状

```go
var doc struct {
    Error *struct {
        Message string `json:"message"`      // 只认这个
    } `json:"error"`
}
```

**同一套接口上，错误不止一种形状**：

| 形状 | 是否识别（修复前） |
|---|---|
| `{"error":{"message":…}}` | ✅ |
| **`{"code":11148,"msg":…}`** | ❌ **完全认不出** |
| **`{"code":…,"message":…}`** | ❌ |
| **`{"error":{"code":…,"msg":…}}`** | ❌ |
| **`{"message":…}`** | ❌ |
| **`{"error_msg":…}`** | ❌ |
| **裸 JSON（无 `data:` 前缀）** | ❌ |

**供应商自己的风格就是 `{"code":…,"msg":…}`** ——
（`11101` 的 `{"code":11101,"msg":"Non-stream chat request…"}` 是同一个家族）

#### 漏认的后果不是「少报一个错」

**这一帧会被当成正常内容转发**：

1. **客户端看到错误文本被当作模型的回答**
2. 流结束时 `emitted == 0` 或内容已污染
3. **最终报出「上游未返回任何内容」** —— **线索指向完全错误的方向**
4. 这类错误**还会被计入账号健康度**（因为认不出是「请求内容问题」）

#### 修复

`extractStreamError` 依次尝试：

```go
nestedMessage(doc.Error),          // {"error":{"message"|"msg":…}}
nestedMessage(response.error),     // {"response":{"error":…}}
doc.Message, doc.Msg, doc.ErrorMsg // 扁平形状
```

`iterSSEPayloads` 同时接受**裸 JSON 行**（无 `data:` 前缀）——
上游在流开始前拒绝请求时会这样回。

**正常内容帧与 `[DONE]` 一律不误判**（误判会把成功的回答变成失败，比漏判更难查）。

### 流式 tool_calls 分片被覆盖（v0.13.33 修复）



**排查依据**：反编译 `AI 聚合网关_0.1.18.apk`（`d2/C0490f.java` 的折叠累加器）。

**流式 `tool_calls` 不是整条发来的**，而是分片：

```
{"index":0,"id":"call_x","function":{"name":"get_weather","arguments":""}}
{"index":0,"function":{"arguments":"{\"city\""}}
{"index":0,"function":{"arguments":":\"Hangzhou\"}"}}
```

**旧实现**：

```go
if len(choice.Delta.ToolCalls) > 0 {
    toolCalls = choice.Delta.ToolCalls     // 整体覆盖
}
```

**每一帧都覆盖前一帧** → **只留下最后一帧的碎片**：

| 字段 | 修复前 | 修复后 |
|---|---|---|
| `id` | ❌ 丢失（首帧才有） | ✅ `call_x` |
| `name` | ❌ 丢失 | ✅ `get_weather` |
| `arguments` | ❌ 只剩最后一片 | ✅ 完整拼接 |

**影响面**：走**折叠路径**的请求（非流式客户端 + 上游不支持非流式时），
**返回的 `tool_calls` 是残缺的** —— 客户端拿到后无法解析参数、无法回填结果。

**修复**：按参考实现的 `toolCallAccumulator`：

- **按 `index` 建槽**（并行调用各自独立，不互相覆盖）
- **`arguments` 拼接**而非替换
- **`id` / `name` / `type` 首帧写入后保留**
- **没有 `id` 的调用丢弃**（客户端无法为它回填 `tool_call_id`）

**实测**：

```
非流式 + 工具调用（走折叠）
  finish_reason: tool_calls
  tool_call id='call_00_b69TzN5rveKloqZClDDu7062'  name='get_weather'
    arguments='{"city": "Hangzhou"}'          ← 完整
```

### tool 对断裂：修补 + 对称裁剪（v0.13.32 修复）



**用户的真实场景**：请求体 **650KB**（完整会话历史），报错

```
tool calls and tool results do not match, please start a new conversation and retry
```

**为什么 v0.13.30 没修好**：那次只覆盖「**id 错位**」，没覆盖「**结果缺失**」。

#### 真实成因：**对话被打断**

用户在 AI 执行工具的过程中**中断**，会话历史里就留下了：

```json
{"role":"assistant","tool_calls":[
  {"id":"call_A", ...},
  {"id":"call_B", ...}        ← 这个没有结果
]},
{"role":"tool","tool_call_id":"call_A","content":"..."}
```

**下一次请求带着这个断裂历史** → 上游拒绝 → **且之后每条消息都失败**（上游注释：
「不完整配对的 tool_calls/tool 结果会让上游对之后每条消息都返 400」）。

#### 四类断裂与处理

| # | 形态 | 处理 | 理由 |
|---|---|---|---|
| 1 | **id 过期**（指向不存在的调用） | **重连**到未回答的调用 | 内容是真实的，只是链接错了 |
| 2 | **id 缺失** | 同上 | 同上 |
| 3 | **有调用无结果** | **裁掉该调用** | 见下 |
| 4 | **孤儿结果**（无对应调用） | **删除该结果** | 无处挂靠 |

**第 3 类为什么是「裁掉」而不是「补空结果」**：

上游要求**每个调用都有结果**，所以必须处理。但补一个占位结果
会被模型当成**真实的工具输出** —— 而**裁掉是纯结构性编辑**，
不往对话里塞任何没发生过的事。

**两侧用同一个 id 集合裁剪**，所以任何一步都不会留下半截配对：

```go
keep := { id | id ∈ 所有 assistant.tool_calls[].id
           且 id ∈ 所有 role:tool 的 tool_call_id }
```

调用侧只保留 `keep` 里的（全空则**删掉 `tool_calls` 键**，不留空数组）；
结果侧同样只保留 `keep` 里的。**assistant 的正文与 reasoning 一律不动**。

#### 顺带：`changed` 标志漏报

重连**不改变配对数量**，所以裁剪那步会认为「无事可做」，
整条规范化被当成 no-op 跳过 —— **修复被静默丢弃**。
现在 `linkToolResultsToCalls` 自己上报是否改写。

**实测**（三个此前失败的场景）：

```
Case3  有调用无结果（打断）  → 正常返回
Case3b 完全无结果            → 正常返回
Case4  孤儿 tool 结果        → 正常返回
```

### 非流式请求把一次成功记成了失败（v0.13.31 修复）



**接上一节**。`11101` 那条路径还有一个**顺序错误**：

```go
if status >= 400 {
    reportExecutorFailure(creds, model, status, respBody)   // ← 先上报失败
}
if isNonStreamUnsupported(status, respBody) {               // ← 再判定 11101
    // 折叠成流式并成功作答
}
```

`11101` 在**上报之后**才被识别，于是**一次成功的调用被记成了账号失败**。

**三次之后账号被停用**，之后所有请求都在拿这个已停的账号重试 ——
而 CPA 每次都报**第一条陈旧的错误**，客户端看到的就是：

```
auth_unavailable: no auth available
(last upstream error: 第一條那个错误)
```

**修复**：把 `11101` 的判定**移到上报之前**。它是**端点能力**问题，不是凭据状态问题，
而下面的折叠会把它变成**成功的响应**。折叠路径**自身**的失败（转流式后上游报错、
零帧）才照常上报。

**双保险**：`11101` 的文案也加入 `isRequestContentFailure`，即使顺序再被改动，
也不会被算成账号故障。

### tool calls 不匹配被误判为账号故障（v0.13.30 修复）



**用户看到的现象**：

```
tool calls and tool results do not match, please start a new conversation and retry
```

更麻烦的是**紧接着**出现：

```
auth_unavailable: no auth available
```

**换账号也没用** —— 于是看起来像账号全挂了。

#### 缺陷 1：`tool_call_id` 从未被校验

`repackToolResults` 只修**相邻性**（结果紧跟在 assistant 之后），但上游还要求
**每个 `role:"tool"` 的 `tool_call_id` 必须对应某个 `tool_calls[].id`**。
客户端在这一步出错很常见：

| 情况 | 例子 |
|---|---|
| **id 过期** | 对话被编辑/回滚后留下的旧 id |
| **缺失 id** | 只写了 `role:"tool"` 和 `content` |
| **并行调用错位** | 两个调用两个结果，id 交叉 |

**修复**：`repairToolCallIDs` 把结果**按顺序重连**到仍未被回答的调用上。

**只改 id，不动内容**：已经正确的配对保持原样，多余的重复结果不去猜（交给错误路径报告）。

#### 缺陷 2：请求的问题算在账号头上

这个错误是 **502 + `server_error`**，与限流长得一样，于是落入软失败计数：
**连续 3 次 → 账号被停**。

**但这明明是请求的问题** —— **换个账号重试同样的对话会同样失败**，
最终把所有账号逐个停掉，客户端就看到 `no auth available`。

上游自己也在提示这是调用方要处理的：

```
please start a new conversation and retry
```

**修复**：`isRequestContentFailure` 识别这类文案，**不计入账号健康度**
（`repairToolCallIDs` 修不了的部分才走到这里）。

覆盖的文案：

| 文案 | 含义 |
|---|---|
| `tool calls and tool results do not match` | 本问题 |
| `request illegal` | 上游对不合规请求的通用拒绝 |
| `invalid_request_error` | OpenAI 风格的请求错误 |

**限流与配额不在其中** —— 它们确实是账号的问题，仍照常冷却。

#### 实测

```
发一个 tool_call_id 不匹配的对话（stale_id_999 → 真实 id 是 call_real_1）
  → {"content":"The current weather in Hangzhou is sunny with a temperature of 24°C…"}

账号状态
  ai: usable=True  账号级冷却=(无)
```

修复前这个请求会失败并把账号停掉；修复后**上游正常接受**，账号**完全不受影响**。

### 账号冷却中是否就「不能用了」（v0.13.29 修复）



**用户看到的现象**：某个模型被限流后，账号看起来**整个不可用**，只能换账号。

**上游的文案其实在提示相反的做法**：

```
您的使用量已超出频率限制，将在 2026-09-27 20:03:46 UTC+8 重置，
您也可以切换其他模型继续使用。      ← 注意这一句
```

**限流是模型级的**，但旧实现把它当成账号级故障。顺藤摸瓜查出**四个层层嵌套的缺陷**，
每一个都足以让账号「看起来不可用」：

#### ① 限流用 502 返回，不是 429

上游的限流响应：

```
HTTP 502
{"error":{"message":"您的使用量已超出频率限制…","type":"server_error","code":"internal_server_error"}}
```

状态码与 OpenAI 的错误词汇**双双指向「通用服务器故障」**，唯一信号是**中文文案**。
`classifyUpstream` 原先只认 429 / `rate_limit_error`，于是限流落进
`failureTransient`：**连续 3 次 → 整个账号冷却 `errorCooldownMillis`**。

**修复**：加入中英文限流短语表，并在 5xx 与 default 分支用文案兜底判断。

#### ② executor 失败时，响应拦截器根本不会被调用

CPA 在 executor 层就判定失败（日志：`conductor_execution.go … upstream execution failed`），
**不会运行响应拦截器**。分类逻辑写在那里，等于**永远不生效** ——
同一个被限流的账号会在**每次请求里被反复重试**。

**修复**：`reportExecutorFailure` 在 executor 内部上报（流式与非流式两条路径）。

#### ③ 限流写在 HTTP 200 的流内帧里

```
HTTP/1.1 200 OK
data: {"error":{"message":"您的使用量已超出频率限制…"}}
data: [DONE]
```

传输层**没有错误**（`errStream == nil`），旧实现把这一帧当**正常内容转发** ——
于是**错误文本被当成模型的回答**显示给用户。

**修复**：在 emit 回调里先查 `extractStreamError`，命中则记下原因、**不再转发**、
并按失败收尾。

#### ④ `classifyUpstream` 只查 JSON 结构

它从 `doc.Error.Message` 读文案。但 ③ 修好后传进去的是**帧里的纯文本**，
JSON 解析拿不到字段 → 文案检查**对着空字符串做** → 又退回「通用 5xx」。

**修复**：`doc.Error.Message` 为空时回退用**整个 body 文本**，
并让 5xx/default 分支统一读 `err.Message`。

#### 修复后的行为

| 场景 | 冷却范围 |
|---|---|
| **限流**（`failureRate`） | **只停该模型**，账号对其他模型照常可用 |
| **配额耗尽**（`failureQuota`） | 账号级（余额是共享的） |
| **认证失败**（`failureAuth`） | 账号级 |

**冷却时长优先采用上游给的重置时刻**（`将在 2026-09-27 20:03:46 UTC+8 重置`），
而不是固定的几分钟：固定值要么在仍被限流时提前放行（每次重试都失败），
要么在已恢复后继续闲置。

> 解析 `UTC+8` 时不能用 Go 的 `Z07:00` layout —— 它只接受 `+08:00` / `+0800`
> 这种带冒号或两位小时的写法，`UTC+8` 会直接解析失败。这里改为手动计算偏移。

上限受 `quota_cooldown_millis` 约束，避免解析异常把一个模型停很久；
提示时刻已过期时（时钟不一致）退回配置时长，而不是当成「未限流」。

面板的账号行会分别显示**账号级冷却**与**模型级冷却**，所以
「账号在服务其他模型」与「账号真的不可用」可以区分开。

### 真流式：必须异步返回（v0.13.28 修复）


**症状**：客户端调用后**长时间无任何输出，然后直接断开**；CPA 日志显示
`200 | 1m59s | POST /v1/chat/completions`（耗时近两分钟但「成功」）。

**根因**：`executor.execute_stream` **把上游的 chunk 全部收集完才返回**。

```go
var chunks []pluginapi.ExecutorStreamChunk
// ...收完整个流...
return okEnvelope(streamChunkEnvelope{Chunks: chunks})
```

于是**整个生成期间客户端零字节**。思考模型（如 `deepseek-v4.1-flash`）首个
token 就要几十秒，客户端等不过自己的读超时，直接断开 —— 看起来就是
「没反应、直接中断」。

**为什么不能改成同步 emit 就算了**：宿主把 emit 的 chunk 放进一个
**16 槽的缓冲**（`pluginhost.streamBridgeBufferSize`），而**消费这个缓冲的
goroutine 要等 `execute_stream` 返回之后才启动**：

```go
queue := make([]pluginapi.ExecutorStreamChunk, 0, 16)
if len(queue) < streamBridgeBufferSize {
    emitC = s.emits        // 队列满 → emitC = nil → emit 永久阻塞
}
```

所以同步实现会在**第 17 个 chunk 卡死**。

**正确做法**（与官方的
`examples/plugin/claude-web-search-router/go/execute_stream.go` 一致）：

```go
streamID := strings.TrimSpace(req.StreamID)
if streamID == "" {
    return errorEnvelope("executor_error", "stream_id is required ...", 400), nil
}
go pumpUpstreamStreamIntoHost(streamID, creds, upstreamBody)   // 后台读上游
return okEnvelope(streamChunkEnvelope{
    Headers: http.Header{"Content-Type": []string{"text/event-stream"}},
})
```

即：**立即返回**，让宿主开始消费；**后台 goroutine** 用
`host.stream.emit` 逐块推送，结束时 `host.stream.close`。这样首个 chunk 一到
就送到客户端。

**顺带修掉的一个隐患**：上游返回错误（如频率限制）时，此前错误会**在缓冲区里
静静等着**，客户端只会看到「挂住然后断开」。现在错误通过 `host.stream.close`
立即传达，客户端能看到具体原因：

```
上游未返回任何内容: 您的使用量已超出频率限制，将在 2026-09-27 20:03:46 UTC+8 重置…
```

**上游半开挂起的防护**：`http.Client` 只设了 `ResponseHeaderTimeout`，响应体
一旦开始读取就**没有任何超时**，上游静默挂起会让读循环永久阻塞。现在加了两道
（`workbuddy_client.go`）：

| 超时 | 值 | 作用 |
|---|---|---|
| `firstByteTimeout` | 6 分钟 | 从开始读 body 到第一个字节的等待上限 |
| `idleLineTimeout` | 3 分钟 | 流运行中两行之间的最长停顿 |

（不能用 `http.Client.Timeout`：它会给整个交换设上限，而思考模型本来就
可能跑好几分钟。）

### 账号落盘（v0.3.1 修复）

`auth_not_found` 表示 CPA 找不到可用于 `codebuddy` 的凭据。v0.3.0 有两个缺陷会导致
**即使登录成功、账号也进不去**：

| 缺陷 | 后果 | 修复 |
|---|---|---|
| `host.auth.save` 字段名用错（传 `FileName`/`StorageJSON`，实际要 `name`/`json`） | CPA 校验失败（`json is required`），**文件根本没写** | 改用 `pluginapi.HostAuthSaveRequest` 的 `name`/`json` |
| 凭据 JSON 缺 `"type"` 字段 | CPA 从 `metadata["type"]` 判断账号属于哪个 provider，缺失则归为 `unknown`，**永远匹配不到 codebuddy** | storageJSON 补 `"type":"codebuddy"` |

正确的落盘数据长这样：

```json
{
  "name": "codebuddy-<uid>.json",
  "json": {
    "type": "codebuddy",
    "accessToken": "...",
    "refreshToken": "...",
    "expiresAt": 1893456000,
    "domain": "cn",
    "uid": "...",
    "enterpriseId": "...",
    "nickname": "..."
  }
}
```

**验证账号是否落盘：**

```bash
docker exec 你的CPA容器名 ls -la /app/auths/ 2>/dev/null | grep codebuddy
# 或
docker exec 你的CPA容器名 find / -name "codebuddy-*.json" 2>/dev/null
```

看到 `codebuddy-*.json` 就说明账号已就位，此时模型即可正常调用。

> 如果登录时插件无法写盘，登录响应里会带上「写入 CPA 账号存储失败：...」的提示，
> 凭据仍会返回，便于手动排查。

## 2.9 单一界面 + 账号直读（v0.6.0）

### 一个页面装下全部

管理面板的 **「AIGW 反向代理」** 入口现在是一个完整页面，包含：

```
AI 聚合网关 · 反向代理
├─ 管理密钥          （存浏览器 localStorage）
├─ WorkBuddy 账号    ← 账号列表 + 一键「签到 + 刷新额度」
├─ 签到设置          （开关 + 每日时间 + 启动补跑）
├─ 额度刷新设置      （开关 + 间隔 + 启动刷新）
├─ 调用统计          （总调用/今日/失败/Tokens + 最近调用）
└─ 网关设置          （api_key 等只读摘要）
```

顶部有锚点导航，单页内跳转。`/checkin` 与 `/quota` 仍保留为**独立视图**
（方便书签），但内容已包含在主页内。

### 账号登录后立即可见

**之前的实现有缺陷**：账号列表读的是插件自己的**凭据池**，而凭据池只在
「发生请求」或「额度刷新」时才被填充 —— 所以刚登录的账号**要等第一次调用才出现**。

**现在改为直读 CPA 的认证存储**（`host.auth.list`），这是账号的权威来源：

```
登录完成 → saveAuthThroughHost() → 主动失效缓存
                                      ↓
              页面重新渲染时直接列出该账号（无需任何调用）
```

对应源 APK 的行为：它是从自己的账号库（`A0.s`）直接列出的，不依赖流量。

### 同一个人只显示一行（v0.13.27）

一个人可能持有**多个凭据**，而它们都是同一个账号：

| 情形 | 说明 |
|---|---|
| **国内 + 国际** | `copilot.tencent.com` 与 `www.workbuddy.ai` **各自签发** token，UID 相同 |
| **凭据无法解析** | 文件损坏时**读不到 UID**，去重键退化为文件名，永远无法与正常那条合并 |

**改进之处**：

1. **按 UID 合并**：同一个 UID 的多条凭据合成一行，并把每个区域与每个
   凭据文件都记录下来：

   ```json
   {
     "uid": "cb56d65f-…",
     "region": "cn",                    // 主区域
     "regions": ["cn", "global"],       // 全部区域
     "auth_indexes": ["codebuddy-…-cn.json", "codebuddy-…-ai.json"],
     "credential_count": 2
   }
   ```

2. **解析失败也参与去重**：从原始 JSON 与 JWT 里尽力恢复 UID。
   否则这条会永远以文件名为身份，和正常的同一个人**并排显示**。

3. **按区域分组**：账号页分成「国内供应商账号」「国际供应商账号」两栏 ——
   两侧行为不同（国际**没有签到**，也**没有成长中心**），
   平铺列表看不出某个功能实际会作用在哪些账号上。

### 只显示 WorkBuddy 账号

严格过滤，**其他供应商（Anthropic / OpenAI / Gemini 等）一律不出现**：

```go
// accounts.go isWorkBuddyAuthEntry
provider 或 type 等于 "codebuddy" 或 "WorkBuddy"      → 收录
auth 文件名以 "codebuddy-" / "codebuddy_" 开头        → 收录
其他                                                  → 过滤掉
```

### 账号状态一目了然

| 列 | 说明 |
|---|---|
| 账号 | 显示名（nickname / label / UID 回退） |
| UID | 供应商侧用户 ID |
| 区域 | `cn` 或 `global`（由凭据 domain 推导，`a2/b.java:284`） |
| 剩余额度 | 已查询则显示数值，否则 `—` |
| 状态 | 可用 / 冷却中（含截止时间）/ 已停用 / 凭据已过期 |

**损坏的凭据文件也会列出来**（状态显示「凭据无法解析」），而不是静默消失 ——
这样你至少知道有这么个文件需要处理。

### 一键操作

页面上的 **「一键：签到 + 刷新额度」** 会依次：

1. 读账号列表
2. 逐个签到
3. 逐个查额度
4. 刷新页面显示结果

对应的 HTTP 接口：

```bash
curl -s -X POST "$BASE/v0/management/workbuddy/run" \
  -H "Authorization: Bearer $KEY"
```

### 账号列表 JSON

```bash
curl -s "$BASE/v0/management/workbuddy/accounts" \
  -H "Authorization: Bearer $KEY"
```

返回 `{accounts, total, usable, credits_known, total_credits, fetched_at, warning}`。

> 列表有 **5 秒缓存**，避免每次渲染都打宿主的 auth 存储。
> 登录成功后会主动失效缓存，所以新账号立刻可见。
> 宿主读取失败时**返回上一次的缓存**并把错误放进 `warning`，页面不会变空。

## 2.10 账号切换策略（v0.7.0）

### 三种策略，面板上一键切换

管理面板的 **「账号切换策略」** 区块：

| 策略 | 行为 | 实现方式 |
|---|---|---|
| **按到期** | 优先使用积分**最快到期**的账号（避免过期浪费） | 插件自己算（8 步门控） |
| **按额度**（默认） | 优先使用**剩余积分最多**的账号 —— 源应用的行为（`A0/s.java:596`） | 插件自己算 |
| **轮巡** | 按顺序轮流使用每个账号 | **委托 CPA 内置调度器** |
| **随机** | 每次随机挑选，避免总是命中同一个账号 | 插件自己算 |

> **轮巡为什么委托给 CPA**：CPA 的内置调度器了解插件看不到的信息（优先级、
> 配额状态、跨插件的账号可见性），且不受插件重载影响。插件通过
> `SchedulerPickResponse.DelegateBuiltin = "round-robin"` 把决定权交回去，
> 比自己维护游标更可靠。

选中后点「应用策略」立即生效，无需重启。另有「重置轮巡位置」把轮巡游标归零。

### 实现原理：CPA 的 `Scheduler` 能力

CPA 正常情况下**自己选账号**。插件通过注册 `Scheduler` 能力接管选号：

```
请求进来
  ↓
CPA 收集所有可用候选账号
  ↓
调用 scheduler.pick(候选列表, 请求上下文)
  ↓
插件按当前策略返回一个 AuthID
  ↓
CPA 用该账号执行请求
```

### 三种策略的算法

```go
// 按额度（A0/s.java:596 的移植）
best = nil
for c in candidates:
    if best == nil || c.Credits > best.Credits: best = c    // 严格 >，同分保持顺序
return best.ID

// 轮巡（按 provider 独立游标）
ids = sorted(candidate ids)
pos = cursor[provider]; cursor[provider] = pos + 1
return ids[pos % len(ids)]

// 随机
return candidates[rng.Intn(len(candidates))].ID
```

### 统一的可用性过滤

三种策略共用同一套筛选，对应 `A0/s.java:596` 的守卫：

```
✓ host 报告的状态不是 disabled / unavailable / failed / invalid / error / expired
✓ host 元数据里没有 disabled: true
✓ 插件凭据池里不在冷却窗口内
✓ 本次请求尚未尝试过（读取 tried_auth_ids）
```

**候选为空时插件返回 `Handled: false`**，把决定权交回 CPA 内置调度器，
不会让请求失败。

### 只接管自己的账号

```go
if provider 是 codebuddy/WorkBuddy       → 接管
if provider 列表里有 codebuddy           → 接管
if 明确指定了别的 provider                → 不接管（交给对应供应商）
if provider 信息为空，但候选都是我们的     → 接管
```

其他供应商的请求**完全不干预**。

### 配置

```yaml
plugins:
  configs:
    workbuddy:
      routing:
        strategy: by_credits    # by_credits | round_robin | random
```

也接受中文/别名：`按额度` / `额度` / `credits`、`轮巡` / `rr`、`随机` / `rand`。

### HTTP 接口

```bash
KEY="你的 management key"; P=workbuddy; BASE="http://127.0.0.1:8317"

# 查看当前策略与选择顺序预览
curl -s "$BASE/v0/management/$P/routing/status" -H "Authorization: Bearer $KEY"

# 切换策略
curl -s -X POST "$BASE/v0/management/$P/routing/config" \
  -H "Authorization: Bearer $KEY" -H 'Content-Type: application/json' \
  -d '{"strategy":"round_robin"}'

# 重置轮巡位置
curl -s -X POST "$BASE/v0/management/$P/reset" -H "Authorization: Bearer $KEY"
```

### 面板上的「选择顺序预览」

按当前策略展示**实际会被选用的顺序**，并显示每个账号**已被选中多少次**，
方便验证策略是否按预期工作：

| # | 账号 | 剩余额度 | 已选中次数 |
|---|---|---|---|
| 1 | acct-a@example.com | 900 | 42 |
| 2 | acct-b@example.com | 500 | 40 |
| 3 | acct-c@example.com | 120 | 41 |

## 2.11 基于 workbuddy-switch 的完善（v0.8.0）

参考实现 [changexbc/workbuddy-switch](https://github.com/changexbc/workbuddy-switch)（Rust/Tauri 桌面 App）
后，修正了本插件的**一个实现错误**并补齐了**两块能力**。

### 2.11.1 修正：国际版的域名与路径（我原来写错了）

| 项 | 国内版 (cn) | 国际版 (ai) |
|---|---|---|
| API 基址 | `https://www.codebuddy.cn` | `https://www.workbuddy.ai` |
| **产品域（Origin/Referer）** | `https://www.codebuddy.cn` | **`https://www.codebuddy.ai`** |
| OAuth platform | `workbuddy` | **`workbuddy-ai`** |
| **billing 路径** | `/v2/billing/meter/...` | **`/billing/meter/...`（无 /v2）** |
| 对话 / 模型列表 | `copilot.tencent.com` | `www.workbuddy.ai` |

**我原来的三处错误：**

1. 国际版产品域写成了 `workbuddy.ai`，**正确是 `codebuddy.ai`**
   —— CodeBuddy 工具链把非产品域识别为"自建部署"，会去读企业端点设置
2. OAuth platform 用了 `CLI`，未区分档位
3. billing 路径未按档位切换

**国际版 billing 的 404 回退**（参考实现实测得出）：

```
先试 /billing/meter/xxx
仅当返回 HTTP 404 时才回落 /v2/billing/meter/xxx
401 / 业务错误码 / 传输错误都**不算**路径问题，不换候选
```

> 设计上采用「档位单一事实来源」：所有档位差异集中在 `variant.go`，
> 其它文件不得出现档位相关的字面量。

### 2.11.2 积分模型升级：从"剩余量"到"剩余量 + 到期时间"

**原来**只查一个旧接口，拿到一个总数。
**现在**优先查三个新接口（旧接口作兜底）：

```
POST {base}/billing/meter/get-user-resource-summary          当前周期用量
POST {base}/billing/meter/get-user-resource-paid-packages    付费包
POST {base}/billing/meter/get-user-resource-free-packages    免费包
```

每个资源包解析为：

| 字段 | 说明 |
|---|---|
| `PackageCode` / `PackageName` | 包标识 |
| `Total` / `Remaining` / `Used` | 数量（缺失时互相推算） |
| `ExpireAt` | **到期时间** |
| `Expired` | 已过期 |
| `ExpiringSoon` | **7 天内到期** |

**到期时间的解析规则**（对应参考实现的 `resolve_expire_at`）：

```
1. 优先 DeductionEndTime / deductionEndTime / ExpiredTime / expiredTime
2. 若 CycleEndTime 比它早超过 365 天 → 改用 CycleEndTime
   （官方数据里 2049 这类是长期占位值，真实到期在周期末）
3. 结果距 now 超过 730 天 → 视为「无到期时间」
```

三个接口可能提到同一个包，插件会**去重**，避免总额被重复累加。

### 2.11.3 新增「按到期」选号策略

参考实现的重要洞察：**选剩余最多的账号不是最优的 —— 积分会过期，
应该先用快到期的。**

新增 `by_expiry` 策略，完整移植其 **8 步决策链**：

```
1. 过滤有效候选（查询成功 + 未过期 + 有剩余）；空 → 不切换
2. 目标 = 到期最早者（无到期时间的排最后）
3. 紧迫度：目标到期剩余 > min_urgency_hours → 不切（都还早）
4. 已是目标 → 不切
5. 冷却期：距上次切换 < cooldown_seconds → 不切
6. 存活门控：有会话在跑 → 不切（活进程持旧凭据，切了不生效）
7. 价值过滤：目标剩余 < min_remaining → 不切
8. 防抖动：目标仅比当前早 min_gap_hours 以内 → 不切
否则切换
```

**每一步拒绝都带可读原因**，便于从面板与日志判断行为是否符合预期。

配置：

```yaml
plugins:
  configs:
    workbuddy:
      routing:
        strategy: by_expiry      # 新增：按到期
        cooldown_seconds: 300    # 切换冷却（默认 5 分钟）
        min_gap_hours: 6         # 防抖动：到期差不足 6 小时不切
        min_urgency_hours: 24    # 都还剩 >1 天就不切
        min_remaining: 0         # 目标剩余低于此值不切（0=关闭）
```

### 2.11.4 面板增强

**账号表**新增两列：

| 列 | 说明 |
|---|---|
| **版本** | `cn` / `ai`，一眼区分国内/国际账号 |
| **到期** | `N 天后`；7 天内显示 ⚠️ 并高亮；已过期标红 |

**概览卡**新增「积分即将/已过期」计数。

**选择顺序预览**在按到期策略下按到期时间排序，并显示每个账号的到期天数。

> 已过期的积分**不再被视为可用账号**，与参考实现的 `Candidate::valid` 一致。

---

#### 手机适配

一个断点（720px，横屏手机约 740 CSS px 以内）覆盖整个手机区间：

- **表格保持横向**，一行一个账号 —— 拆成「每个字段一行」的堆叠布局会让列表高得
  离谱，而找某个账号时需要的正是整行一眼扫过
- **溢出限定在表格内部**（`.table-wrap` 自带横向滚动）：让页面本身横向滚动会把
  标签栏和标题一起带走
- **触摸目标下限 42px**：桌面尺寸是按鼠标调的，大约只有 30px
- **输入框字号 16px**：更小的字号会让 iOS 在聚焦时自动缩放，之后页面就停在放大状态
- **提示条改为左右贴边**：固定在右侧时，长消息在窄屏上会被裁掉

**操作列改用图标按钮**是这套适配里最关键的一步 —— 三个带文字的按钮在手机宽度下
放不进最后一列，那一行会溢出、只有第一个按钮能点到。图标只要三分之一宽度。

真机实测（457px CSS 宽 / 1080px 物理宽）：

| 项 | 改造前 | 改造后 |
|---|---|---|
| 操作按钮可点数 | 1 / 3 | **3 / 3** |
| 按钮触摸区域 | 65px | **84px** |
| 筛选条右侧空隙 | 44px | **0** |

#### 成长任务结果改为分组卡片


原先一次运行的日志被拼进一个 `<pre>`：成功、跳过、失败交织在一起，失败条目还带着
完整的上游 JSON。**被同一个前置条件挡住 16 个任务**的运行，于是产生十六屏几乎相同
的文字，而真正解释原因的那一行埋在里面。

现在按等级分组呈现：

```
┌─ 成长任务结果            1 个账号 · 累计 +0 积分 ─┐
│    [2]      [2]      [16]     [1]                 │
│    完成     跳过     未成功   提示                 │
│                                                   │
│  ● 完成   ○ 跳过（无法代做或不在时段）             │  ← 图例
│  ● 未成功（可展开看原因）   ○ 说明                 │
│                                                   │
│  ▼ ● 未成功  16 条                                │  ← 默认展开
│    ● 接取 create_canvas 被拒: ...                 │  ← 限高滚动
│  ▶ ○ 跳过  2 条                                   │  ← 默认收起
│  ▶ ● 完成  2 条                                   │
└───────────────────────────────────────────────────┘
```

三条规则：

- **先数字后细节** —— 四个计数器在上，条目在下
- **「未成功」默认展开**（它是需要行动的那组），**超过 6 条时限高 320px 滚动** ——
  否则一次运行几十个失败会把上面的汇总挤出屏幕
- **图例必须有** —— 没有它，`●` / `○` 只是装饰，读者得猜

**条目一条不丢**：跳过要能看到原因，而原因是上游原文，面板转述只会丢细节。

---

### 2.11.5 用量趋势、账号筛选与逐账号操作

参考同类面板（[workbuddy2api-panel](https://github.com/linguo2625469/workbuddy2api-panel)）
的做法补齐了三处：**趋势图**、**账号筛选**、**操作反馈**。

#### 用量趋势（最近 7 天）

统计页新增一张按天汇总的柱状图：

- **失败堆叠在成功之上**，柱高按当天总调用量成比例，一眼看出失败占比
- **没有流量的一天画成细底线**，而不是 0 高度柱 —— 安静的周末不该读成上游故障
- 悬停显示当天明细（调用数、失败数）
- 数据来自 `/status` 的 `usage_daily`，**仅在统计页可见时拉取**：
  7 天数据变化很慢，在账号页反复请求只是徒增管理接口流量

**为什么自己画 SVG**：面板是内嵌的、必须自包含，引入图表库意味着多一份要跟随
CPA 版本维护的资产。这张图只需要矩形和线条。

**为什么只留 7 天**：桶按天追加，不设上限会让历史无限增长；7 天足够看出走势，
也能在手机上完整放下而不横向滚动。

#### 账号搜索与筛选

账号列表上方是一条筛选栏：

- **搜索框** —— 匹配账号名 / UID / 认证索引 / 版本 / 停用原因，带放大镜图标，
  **输入后右侧出现清除按钮**（点一下清空并把光标留在框里）
- **状态筛选** —— 全部 / 仅可用 / 仅冷却中 / 仅停用 / 仅凭据异常
- **计数** —— 有筛选条件时显示「显示 N / M」，靠右对齐

搜索框用弹性宽度吃掉剩余空间，下拉按内容定宽 —— 早先的写法把输入框钉成固定宽度，
在手机宽度下右侧会剩下约 40px 死区。

筛选在**客户端**完成：列表已经在页面上，每次按键都往返一次管理接口会更慢，
还会打断正在输入的焦点。

状态归类由与状态标签**相同的判断**推导，两者不会各说各话。

#### 逐账号快捷操作（图标按钮）

账号表每行右侧是三个图标按钮：

| 图标 | 动作 | 请求 |
|---|---|---|
| ⏻ | 启用 / 停用该账号 | `POST /account/toggle` |
| ✓ | 只签到该账号 | `POST /checkin/run?uid=<uid>` |
| ¥ | 只刷新该账号的积分 | `POST /quota/refresh?uid=<uid>` |

省略 `uid` 即为原有的全量行为。两种刷新**共用同一把忙碌标志** ——
账号级的刷新同样会访问上游，与全量刷新并行只会让同一个凭据承受双倍请求。

**为什么是图标而不是文字**：三个带文字的按钮（「启用」「签到」「积分」）需要约
250px，塞不进手机宽度下表格的最后一列 —— 那一行会溢出，只有第一个按钮还能点到。
图标只要三分之一宽度，于是整行放得下、三个动作都能点。每个按钮都带 `title` 与
`aria-label`，图标本身有歧义，读屏软件没有可访问名就什么都读不出来。

#### 操作反馈不再刷新页面

原先操作完成后靠重写一行文字并 `location.reload()` 告知结果，这会让操作者
丢失滚动位置和当前标签页，**失败时也一样**。现在改为角落的提示条：
成功 3.6 秒、失败 6 秒后自动消失，内容用 `textContent` 写入
（错误信息可能带着上游原文）。

---

## 2.12 本地端到端实测与修复（v0.8.6）

在沙箱内**真实搭建了 CPA + 插件**并完成一次完整登录与调用，验证并修掉了 6 个只有在真机
运行时才会暴露的缺陷。

### 实测环境

| 项 | 值 |
|---|---|
| CPA | 本地编译 v7.3.15（`CGO_ENABLED=1 go build ./cmd/server`） |
| 插件 | 放入 `plugins/linux/arm64/workbuddy.so` |
| 登录 | 设备码：`GET /v0/management/codebuddy-auth-url` → 浏览器授权 → `GET /v0/management/get-auth-status?state=...` |
| 账号落盘 | `/root/.cli-proxy-api/codebuddy-<uuid>.json` |

### 实测结果（全部通过）

```
✓ 设备码登录        浏览器授权后 status=ok
✓ 账号注册          provider=codebuddy，quota_provider=codebuddy
✓ 模型列表          30 个模型，含 deepseek-v4.1-flash
✓ 流式对话          delta.content 返回真实内容
✓ 非流式对话        aggregate 成功，返回 chat.completion
✓ 额度查询          周期剩余 1890（真实数据）
✓ 签到              今日已签到（幂等识别正确）
✓ 策略切换          by_expiry / round_robin / random / by_credits
```

### 本轮修掉的 6 个缺陷

| # | 缺陷 | 症状 |
|---|---|---|
| 1 | **资源路由 `Path: "/"` 被 CPA 拒绝** | 面板菜单项不显示（CPA 用 `TrimRight(path,"/")`，空串即拒） |
| 2 | **管理路由前缀理解错误** | ManagementRoute 需自带 `/<plugin-id>/` 前缀（CPA 不拼）；ResourceRoute 则相反（CPA 会拼） |
| 3 | **缺失 `routing/*` 三个路由** | 策略切换按钮点了没反应（前端调用但未注册） |
| 4 | **上游不支持非流式对话** | `{"code":11101,"msg":"Non-stream chat request is currently not supported"}` → 现改为内部转流式再聚合 |
| 5 | **「今日已签到」被误判为失败** | 上游用 **HTTP 400 + code=10001 + 中文文案**，原判定在非 2xx 分支直接返回失败 |
| 6 | **额度查到了但面板显示 0** | 写入按 auth index、读取按 uid，三个 key 不一致；改为多 key 匹配 |

另外加固了一处**会导致插件被熔断**的风险：CPA 会把 panic 过的插件加入 `fused` 集合，
此后**所有能力（含 model_router）都被跳过**，表现为「之前能用、突然 unknown provider」。
新增 `safe.go`，把 panic 兜在局部并打日志，不再让一次异常瘫痪整个插件。

### 关于非流式

WorkBuddy 上游**只接受流式请求**。插件收到非流式请求时会：

1. 先把请求改成 `stream: true`（并补 `stream_options.include_usage`）
2. 流式调用上游
3. 把 SSE 帧**聚合成一个 `chat.completion`** 返回给客户端

包含 `delta.content` 拼接、`finish_reason` 取末帧、`usage` 透传，以及中途错误帧的识别。

---

## 3. 获取与构建

### 方式 A：直接下载预编译插件（最快）

从本仓库的 **Releases** 页下载对应架构的 `.so`，文件名统一为 `workbuddy.so`，
按 release 标题区分 `linux/amd64` 或 `linux/arm64`。

下载后直接跳到 [第 4 节](#4-安装到-cliproxyapi)。

> ⚠️ `.so` 是原生库，**必须与 CPA 所在机器的 CPU 架构匹配**。
> 用 `uname -s -m` 确认：`Linux aarch64` → arm64，`Linux x86_64` → amd64。

### 方式 B：从源码构建

```bash
git clone https://github.com/<你的用户名>/workbuddy-cpa-plugin.git
cd workbuddy-cpa-plugin

# 依赖：Go 1.26+ 与 C 编译器（cgo 必需 —— 插件是 c-shared 动态库）
go mod download

# 生产插件
CGO_ENABLED=1 go build -buildmode=c-shared -o workbuddy.so .

# 单元测试（32 个用例：默认值/路由/鉴权/冷却/SSE/管理端）
CGO_ENABLED=0 go test ./...

# dlopen 端到端冒烟测试（真实加载 .so 并走完一遍 RPC 会话）
CGO_ENABLED=1 go build -o smoke-bin ./smoke
./smoke-bin workbuddy.so
```

**构建环境要点**：

- **必须有 C 编译器**：`gcc`（Debian/Ubuntu: `apt install build-essential`）或 `clang`。
  用 `CGO_ENABLED=0` 会报 `-buildmode=c-shared requires external (cgo) linking`。
- **不建议交叉编译**：cgo 交叉编译需要目标平台的 C 工具链，很麻烦。
  要跨架构，直接用本仓库已配好的 GitHub Actions（`.github/workflows/build.yml`）——
  打 tag 会自动在 amd64 与 arm64 runner 上分别编译、跑测试与冒烟、并附到 Release。
- **最省事的做法**：直接在跑 CPA 的那台机器上 `go build`。

产物约 7.5 MB，`-buildmode=c-shared`，仅依赖 `libc` / `libresolv`。

导出的 ABI 符号（必须齐全）：

```
cliproxy_plugin_init
cliproxyPluginCall
cliproxyPluginFree
cliproxyPluginShutdown
```

自检：
```bash
nm -D --defined-only workbuddy.so | grep cliproxy_plugin_init
```


---

## 4. 安装到 CLIProxyAPI

CPA 有 **两种**装上插件的方式。

### 方式一：插件商店（推荐 —— 配置里加个链接就能拉）

CPA 内置 plugin store，会从 `registry.json` 清单自动下载并安装插件。
默认源是官方的，用 `store-sources` **追加你自己的源**即可。

**第 1 步：把 Release 资产传上去**

打 tag 推送后，GitHub Actions 会**自动**编出两个架构的包并发布 Release：

```bash
git tag v0.1.0
git push origin v0.1.0
```

Release 上应出现 3 个文件（CI 自动生成，格式由 CPA 校验过）：

```
workbuddy_0.1.0_linux_arm64.zip
workbuddy_0.1.0_linux_amd64.zip
checksums.txt
registry.json
```

> ⚠️ 资产名格式是 CPA 硬性要求：`<id>_<version>_<goos>_<goarch>.zip`，
> 且必须带 `checksums.txt`。改了名字商店就找不到，会报
> `release asset ... not found`。

**第 2 步：把 `registry.json` 放到公网可访问的地址**

最简单是传到你仓库的 `main` 分支根目录，然后用 raw 链接。或者直接用
GitHub Pages / 任意静态托管。

**第 3 步：在 CPA 配置里加源**

```yaml
plugins:
  enabled: true
  dir: "plugins"
  # 追加第三方源（官方源仍是默认内置的，不需要重复写）
  store-sources:
    - "https://raw.githubusercontent.com/<你的用户名>/workbuddy-cpa-plugin/main/registry.json"
  configs: {}
```

**第 4 步：在 CPA 的 WebUI 里安装**

打开 CPA 管理面板 → 插件 → 插件商店，应该能看到 **AIGW 反向代理**，
点安装即可。CPA 会：
1. 拉 registry.json → 找到插件条目
2. 查你的 GitHub Release → 按当前 `GOOS/GOARCH` 匹配对应 zip
3. 下载 + 校验 `checksums.txt` 里的 sha256
4. 解包 → 把 `workbuddy.so` 落到 `plugins/linux/<arch>/`

装完在 `configs` 下加插件配置（见下方「配置字段」），重启生效。

> 如果源需要认证（私有仓库），用 `store-auth` 配置凭据。

---

### 方式二：一键安装脚本（推荐用于升级）

商店那条路会经过两处缓存/校验，升级时常遇到「商店里还是旧版本」：

- CPA 把最新 release 的查询结果缓存 **1 小时**（`pluginReleaseCacheTTL`），
  期间打开商店看到的仍是旧版本号；
- `direct` 形式的清单为产物钉死 sha256，同一个 tag 重新构建就再也对不上，
  商店会以 `artifact checksum mismatch` 拒绝安装。

脚本绕开这两者，直接从 release 取产物：

```bash
cd /path/to/cpa          # CPA 工作目录（config.yaml 所在目录）
curl -fsSL https://raw.githubusercontent.com/Lxapk/workbuddy-cpa-plugin/main/install.sh | sh
```

指定版本或目标架构（本项目的 CI 只发布 `linux/amd64`，在 arm64 机器上准备
x86_64 服务器用的文件时需要显式指定）：

```bash
VERSION=0.13.40 GOARCH=amd64 sh install.sh
DRY_RUN=1 sh install.sh        # 只看会做什么，不落盘
```

脚本会：读取 `config.yaml` 里的 `plugins-dir`（默认 `plugins/`）→ 下载对应
平台的产物 → 有 `checksums.txt` 就核对 sha256 → 解包出 `workbuddy.so` →
以「先写临时名再改名」的方式落盘，运行中的 CPA 不会读到半截文件。

装完**重启 CPA**生效；插件面板上的版本号会显示成 `install.sh` 装入的那一版。

### 方式三：手动放置 `.so`

不走商店，直接手动放文件。

**先理解文件名的规则**（这一步很容易踩坑）：

> CPA 用**动态库文件名去掉扩展名**作为插件 ID。
> ID 必须匹配 `[A-Za-z0-9][A-Za-z0-9._-]{0,127}`。
> `plugins.configs` 里的键**必须与这个 ID 完全一致**，否则插件不会被加载，
> 列表里显示 `registered=false`、模型与账号全都为空。

| 文件名 | 推导出的插件 ID | `configs` 里要写 | 结果 |
|---|---|---|---|
| `workbuddy.so` | `workbuddy` | `workbuddy` | ✅ 正常 |
| `workbuddy-v0.13.24.so` | `workbuddy-v0.13.24` | 需要同名 | ⚠️ 与 `workbuddy` 是两个插件 |
| `workbuddy-0.13.10.so` | `workbuddy-0.13.10` | 需要同名 | ⚠️ 同上 |

**所以不要给文件加版本号后缀**。Release 里的 zip 解出来就是 `workbuddy.so`，
直接用它即可；更新时覆盖同一个文件。

> **为什么加版本号会出问题**：`workbuddy-0.13.10.so` 会被当成一个 ID 叫
> `workbuddy-0.13.10` 的**独立插件**，与 `workbuddy` 并存。于是删掉
> `workbuddy` 之后列表里还有一项、看起来"删不掉"，而它又因为
> `configs` 里没有对应条目而永远不会加载。

**放置位置**（宿主按这个顺序搜索，两处都支持）：

```bash
cd /path/to/cpa          # CPA 工作目录（config.yaml 所在目录）

# 推荐：架构子目录
mkdir -p plugins/linux/arm64      # 或 plugins/linux/amd64
cp workbuddy.so plugins/linux/arm64/

# 或者：直接放根目录
cp workbuddy.so plugins/
```

```yaml
plugins:
  enabled: true
  dir: "plugins"
  configs:
    workbuddy:            # ← 【必须与文件名一致，且必须显式列出】
      enabled: true       # ← 【必须显式启用，否则不加载】
      priority: 10
      api_key: [REDACTED]
      allow_no_key: false
      default_provider: "codebuddy"
      max_rotate: 3
      error_threshold: 3
```

---

### 配置字段

两种方式共用同一套字段（对应源应用 `V1/s`）：

| 字段 | 默认 | 说明 |
|---|---|---|
| `port` | 8790 | 源应用监听端口；**仅展示**，实际端口由 CPA 决定 |
| `api_key` | `""` | 客户端 Bearer 令牌 |
| `allow_no_key` | `true` | 免鉴权放行（生产建议 `false`） |
| `expose_lan` | `true` | 仅展示 |
| `only_usable_models` | `false` | 仅展示 |
| `refresh_skew_seconds` | 86400 | 提前刷新凭据的秒数 |
| `max_rotate` | 3 | 每请求最大换号次数 |
| `quota_cooldown_millis` | 43200000 | auth/rate/quota 拒绝后的硬冷却（12h） |
| `soft_cooldown_millis` | 60000 | 单次瞬时失败的软冷却（60s） |
| `error_threshold` | 3 | 连续失败多少次才停用 |
| `error_cooldown_millis` | 600000 | 达阈值后的停用时长（10min） |
| `log_retention_days` | 30 | 日志保留天数 |
| `default_provider` | `"codebuddy"` | 无 `provider/` 前缀时使用的供应商。源应用默认是 `"trae"`，本插件改为自身的 provider key |
| `enforce_default_provider` | `false` | **插件新增**：只允许默认供应商 |
| `debug` | `false` | 详细日志 |

---

### 客户端调用

```bash
# 默认供应商
curl http://127.0.0.1:8317/v1/chat/completions \
  -H "Authorization: Bearer sk-your-gateway-key" \
  -H "Content-Type: application/json" \
  -d '{"model":"glm-5.2","stream":true,"messages":[{"role":"user","content":"hi"}]}'

# 显式指定供应商（前缀会被插件剥掉）
curl http://127.0.0.1:8317/v1/chat/completions \
  -H "Authorization: Bearer sk-your-gateway-key" \
  -H "Content-Type: application/json" \
  -d '{"model":"openai/gpt-4o","messages":[{"role":"user","content":"hi"}]}'
```

### 状态面板

```
/v0/resource/plugins/workbuddy/status
```
- `Accept: text/html` → 可视化面板（调用统计 / 网关设置 / 账号池冷却 / 最近调用）
- 否则 → JSON

附加管理路由：
```
GET /v0/management/workbuddy/status   # 同上 JSON
GET /v0/management/workbuddy/calls    # 最近 50 条调用记录
```

### 排障

| 现象 | 原因 |
|---|---|
| 商店里看不到插件 | `store-sources` 的 URL 不可达，或 `registry.json` 格式错（`schema_version` 必须为 2） |
| `release asset ... not found` | Release 资产名不符合 `<id>_<version>_<goos>_<goarch>.zip` |
| `release asset checksums.txt not found` | Release 里没传 `checksums.txt` |
| `checksum mismatch` | zip 被改动过，或 `checksums.txt` 未同步更新 |
| `dynamic library filename must be ...` | zip 内的 `.so` 名字不对或不在根目录 |
| **`target dynamic library must be at zip root`** | **zip 内的 `.so` 带了目录前缀**（例如打成 `linux/amd64/workbuddy.so`）。商店要求归档里**只有一个**、**位于根目录**的 `<id>.so`；宿主推荐的 `plugins/<GOOS>/<GOARCH>/` 布局是**安装之后**由宿主落盘时决定的，与 zip 结构无关 |
| `zip contains multiple target dynamic libraries` | 归档里有多个 `.so`。只放一个 |
| 日志没有 `plugin loaded` | 架构不匹配（用 `uname -m` 核对）、或 `plugins.enabled` 没开 |
| 请求全部 401 | `api_key` 已设且 `allow_no_key: false`，但客户端没带 `Authorization: Bearer <key>` |


---

## 5. 验证状态

| 项目 | 结果 |
|---|---|
| 编译 `c-shared` .so | ✅ `dist/workbuddy.so` |
| ABI 符号导出 | ✅ `nm -D` 四个符号齐全 |
| 单元测试 | ✅ 32/32 通过 |
| `dlopen` + `cliproxy_plugin_init` | ✅ 返回 0，abi_version=1 |
| `plugin.register` | ✅ 15 个 ConfigFields + 6 项 capabilities |
| `frontend_auth.authenticate` | ✅ 无 key → 401 `invalid_api_key`；正确 key → 通过 |
| `request.intercept_before` | ✅ `openai/gpt-4o` → provider=openai, model=`gpt-4o`，stream 保留 |
| `response.intercept_after` | ✅ 记账成功，usage 计数正确 |
| `response.intercept_stream_chunk` | ✅ header-init 正常 |
| `usage.handle` | ✅ 凭据登记 + usage 累计 |
| `management.handle` | ✅ port 9100 生效、api_key 脱敏、calls=2、tokens=24/14、accounts=1 |
| `cliproxy_plugin_shutdown` | ✅ 干净返回 |

> 说明：以上为**插件侧 ABI 级端到端验证**（真实 `dlopen` 加载 .so 并完整跑通 RPC 会话），
> 尚未在真实 CPA 进程 + 真实上游账号下端到端跑通一次实际模型调用。

---

## 6. 源码结构

```
workbuddy-cpa-plugin/
├── go.mod                    模块 github.com/taixu/workbuddy
├── cabi.go                   C ABI（dlopen 入口 + 缓冲区管理）
├── rpc.go                    RPC 分发 + 注册元数据 + 上游失败分类
├── settings.go               gatewaySettings（V1/s 复刻）+ YAML 解码
├── routing.go                resolveRoute / rewriteModelBody（V1/o.k 6-8）
├── frontendauth.go           常量时间 Bearer 校验（V1/o.j）
├── pool.go                   账号池 + 冷却策略（A0.s + V1/k.c）
├── intercept_request.go      路由改写 + 头戳记
├── intercept_response.go     失败分类 + 记账（V1/o.k 9 + V1/o.r）
├── stream.go                 SSE 解析 + usage 累计（V1/o.p + V1/m）
├── usage.go                  callRecord / callLog / usagePayload
├── usage_handler.go          UsagePlugin 钩子
├── management.go             /status JSON + HTML 面板
├── yaml.go                   配置解码封装
├── plugin_test.go            32 个单元测试
├── smoke/main.go             dlopen 端到端冒烟测试
├── scripts/build_release.py  打包插件商店所需的 zip + checksums.txt
├── scripts/gen_registry.py   生成插件商店用的 registry.json
├── .github/workflows/build.yml   CI：多架构编译 + 测试 + 冒烟 + 打包 Release
└── .gitignore
```

---

## 7. 已知差异与取舍

1. **`port` 字段**：源应用自己监听 8790；插件由 CPA 监听，该字段仅作展示，不参与绑定。
2. **`expose_lan` / `only_usable_models`**：仅登记在状态面板，实际绑定与模型可见性由 CPA 控制。
3. **`enforce_default_provider`**：插件新增开关（源应用总是接受显式 `provider/` 前缀），默认关闭。
4. **账号池轮换次数**：源应用在网关内硬控 `maxRotate`；插件改为把冷却策略交给 CPA 的
   轮换机制，`max_rotate` 仅用于展示。若需要严格等价，可在 CPA 的 provider 配置里限制重试次数。
5. **出站代理 `V1/z` ProxySettings**：源应用另有出站 HTTP/SOCKS 代理设置（host/port/user/pass/
   excludedProviderIds），与「反向代理」无关，插件未包含；CPA 自身支持 `proxy-url` 配置。

---

## 8. License

插件代码为本项目原创移植实现。CLIProxyAPI 的 SDK（`sdk/pluginapi`、`sdk/pluginabi`）
按其仓库 License 使用。
