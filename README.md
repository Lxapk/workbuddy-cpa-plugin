# WorkBuddy CPA 插件

[CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI)（CPA）v7 的原生动态插件（`.so`），把 WorkBuddy / CodeBuddy 账号反代为 **OpenAI 兼容接口**，并在 CPA 管理面板里提供账号、积分、签到与任务管理。

## 功能

- **OpenAI 兼容调用**：`/v1/chat/completions`（流式 / 非流式）与 `/v1/models`，模型目录自动从上游拉取。
- **多账号轮换**：按到期、按额度、轮巡、随机四种策略；失败分类冷却、限流自动换号。
- **国内 / 国际双区域**：自动识别账号所属区域；可限定调用只用国内或国际账号，切回后自动恢复。
- **账号管理**：一键启用 / 禁用（直接写入 CPA 的 auth 文件，与 CPA 面板状态一致）、逐账号查余额。
- **积分**：国内、国际账号的剩余积分与到期时间，汇总展示。
- **每日签到与成长任务**（国内版）：签到、成长任务、猫猫旅行、打卡福利，支持定时执行与启动时补跑。
- **管理面板**：统计、用量趋势、最近调用、运行日志，适配手机端；时间统一按北京时间显示。

## 安装

### 方式一：插件商店

在 CPA 配置里追加本仓库的清单源：

```yaml
plugins:
  enabled: true
  dir: "plugins"
  store-sources:
    - "https://raw.githubusercontent.com/Lxapk/workbuddy-cpa-plugin/main/registry.json"
```

打开 CPA 管理面板 → 插件 → 插件商店，找到 **WorkBuddy** 安装后重启 CPA。

> CPA 会缓存最新版本信息约 1 小时，刚发布的版本可能稍后才出现在商店里。

### 方式二：安装脚本

在 CPA 工作目录（`config.yaml` 所在目录）执行：

```bash
curl -fsSL https://raw.githubusercontent.com/Lxapk/workbuddy-cpa-plugin/main/install.sh | sh
```

可选环境变量：

```bash
VERSION=0.1.2 GOARCH=amd64 sh install.sh   # 指定版本 / 架构
DRY_RUN=1 sh install.sh                    # 只显示将执行的操作
```

脚本会下载 Release 产物、核对 `checksums.txt`，并以原子替换方式写入 `plugins/`，装完重启 CPA 生效。

> 当前 CI 只发布 `linux/amd64` 产物。

## 使用

1. 在 CPA 的 OAuth 登录页完成 WorkBuddy / CodeBuddy 授权，账号会自动出现在插件面板。
2. 客户端按 OpenAI 格式调用 CPA：

```bash
curl http://127.0.0.1:8317/v1/chat/completions \
  -H "Authorization: Bearer <你的 CPA API Key>" \
  -H "Content-Type: application/json" \
  -d '{"model":"glm-5.2","stream":true,"messages":[{"role":"user","content":"hi"}]}'
```

3. 管理面板：CPA 管理面板 → 插件 → WorkBuddy，包含「账号」「任务」「日志」「设置」四页。

## 配置

在 `plugins.configs.workbuddy` 下填写，均为可选：

| 字段 | 默认 | 说明 |
|---|---|---|
| `refresh_skew_seconds` | 86400 | 凭据提前刷新的秒数 |
| `max_rotate` | 3 | 单次请求最多换号次数 |
| `quota_cooldown_millis` | 43200000 | 鉴权 / 限流 / 额度拒绝后的冷却（12h） |
| `soft_cooldown_millis` | 60000 | 单次瞬时失败的冷却（60s） |
| `error_threshold` | 3 | 连续失败多少次后暂停账号 |
| `error_cooldown_millis` | 600000 | 达到阈值后的暂停时长（10min） |
| `log_retention_days` | 30 | 日志保留天数 |
| `default_provider` | `codebuddy` | 模型名无 `provider/` 前缀时使用的供应商 |
| `debug` | `false` | 详细日志 |

路由策略、区域开关、签到与任务的定时等在面板「设置」「任务」页修改，自动保存。

## 构建

依赖 Go 1.26+ 与 C 编译器（cgo，插件为 c-shared 动态库）：

```bash
CGO_ENABLED=1 go build -buildmode=c-shared -trimpath -o workbuddy.so .
go test ./...                      # 单元测试
sh scripts/run_smoke.sh amd64      # dlopen 端到端冒烟测试
python3 scripts/validate_registry.py
```

发布：修改 `rpc.go` 的 `pluginVersion` 与 `registry.json` 的 `version` / `release_tag`，推送 `v<版本>` tag，GitHub Actions 自动测试、打包并创建 Release。

## License

MIT
