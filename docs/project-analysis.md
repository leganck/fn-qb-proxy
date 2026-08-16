# fn-qb-proxy 项目分析

> 分析日期：基于当前 main 分支（`2c54ae7a`）与工作区未提交改动。
> 分析范围：`proxy/`、`http/`、`sigctx/` 三个 Go 包，以及构建/发布配置。

## 一、项目定位

`fn-qb-proxy` 是基于 [fnos-qb-proxy](https://github.com/xxxuuu/fnos-qb-proxy) 修改的 qBittorrent 代理工具集，专用于 fnOS 系统。它解决三个问题：

1. **WebUI 默认关闭**：fnOS 中 qBittorrent 的 WebUI 默认不开启，无法通过浏览器管理。
2. **动态随机密码**：qBittorrent 进程启动时会动态生成随机 WebUI 密码，用户不知道也无法固定。
3. **Unix Socket 访问限制**：qBittorrent 通过本地 Unix Socket 通信，外部应用（如 MoviePilot、NasTools）无法直接访问。

方案是：后台守护进程自动探测 fnOS 中运行的 qBittorrent 进程，从进程命令行中提取动态密码与 Socket 路径，为每个系统用户创建独立的代理 Socket；外部应用通过 HTTP 网关间接访问，代理层自动注入动态密码完成登录。

## 二、整体架构

```
fn-qb-proxy（后台守护进程，每 5s 轮询一次）
┌─────────────────────────────────────────────────────────────┐
│ credential.go                                                │
│   pgrep -f qbittorrent-nox  → PID 列表                       │
│   ps -o user= -p <pid>       → 系统用户名                    │
│   正则解析命令行 --webui-password= / --webui-sock-path=       │
│   → 重建 credentials 映射 → 与旧凭据 diff → 发 add/remove 事件 │
├─────────────────────────────────────────────────────────────┤
│ proxy.go                                                     │
│   事件监听 → 为每个用户创建独立 Unix Socket 反向代理           │
│   自动注入动态密码登录；拦截 logout 与 setPreferences（防改密/ │
│   改用户名/退出导致失联）                                    │
└─────────────────────────────────────────────────────────────┘
        │ 每个用户一个 /run/fn-qb-proxy/<user>-qb-proxy.sock
        ▼
fn-qb-http（可选 HTTP 网关，Docker 部署）
   单 UDS → HTTP :18080，支持宽松/严格密码校验
```

依赖极简：`github.com/sirupsen/logrus` + `github.com/urfave/cli/v2`，Go 1.23。`CGO_ENABLED=0` 静态编译，支持 linux amd64 / arm64，goreleaser 打 tar.gz 并推送 GHCR 多架构镜像。

## 三、组件详解

### 3.1 fn-qb-proxy（`proxy/`）

**凭据发现（credential.go）**

- 每 5 秒全量扫描一次：`pgrep -f "qbittorrent-nox"` 拿 PID；`pgrep` 返回 exit status 1 表示无进程，属正常情况直接返回。
- 对每个 PID 用 `ps -o user=` 取系统用户名，用 `ps -p <pid> -o command=` 取完整命令行。
- 正则提取：
  - `--webui-password=(\S+)`
  - `--webui-sock-path=(\S+)`
- 重建 `newCredentials` 映射后与旧映射 diff：
  - 用户消失 → 发 `remove` 事件；
  - 凭据变化 → 检查新 socket 是否就绪（`test -S`），就绪则 `remove` + `add`，未就绪则只 `remove`；
  - 新用户出现 → 发 `add` 事件。
- 最后整体替换 `credentials` 映射（读写均有 RWMutex 保护）。

**用户代理管理（proxy.go）**

- `userEvents` 通道（容量 10）承载 add/remove 事件，事件处理器线程在 `startHTTPServer` 中消费。
- 每个用户创建一个监听 `getProxySocketPath(username)` 的 `http.Server`，Transport 的 `DialContext` 直连 qBittorrent 原生 Unix Socket。
- `Rewrite` 阶段拦截三类请求：
  - `/api/v2/auth/login`：把请求体改写为 `username=admin&password=<动态密码>`，自动登录；
  - `/api/v2/auth/logout`：直接返回 `Ok.`，防止登录态被退出；
  - `/api/v2/app/setPreferences`：若参数含 `web_ui_password` / `web_ui_username` 直接 403，防止改密/改用户名导致代理失联。
- `removeUserProxy`：Shutdown（5s 超时，超时强制 Close）→ 删除 socket 文件 → 从映射移除。
- `cleanupAllProxies`：进程退出时统一清理。

**CLI（main.go）**

- 全局参数：`--debug/-d`、`--socket-perm/-sp`（默认 0660）、`--proxy-socket-dir/-psd`（默认 `/run/fn-qb-proxy`，支持 `PROXY_SOCKET_DIR` 环境变量）。
- 子命令 `service install/uninstall/start/stop/restart`：管理 systemd 服务。

### 3.2 fn-qb-http（`http/`）

- 单 UDS → HTTP 反向代理，`DialContext` 直连指定 Unix Socket（默认 `/home/admin/qbt.sock`，可经 `UDS` 覆盖）。
- 密码校验规则：
  - `--password` 为空（宽松模式）：body 原样透传，qBittorrent 原生校验；
  - `--password` 非空（严格模式）：只有输入的密码与配置一致才登录成功（错误密码原样透传给 qBittorrent，由其拒绝；空密码或正确密码都改写为配置密码登录）。
- 日志使用 JSONFormatter，适配 Docker/systemd 采集。
- 参数均可用环境变量配置：`UDS` / `PORT` / `PASSWORD` / `DEBUG`，docker-compose 部署时推荐环境变量方式。

### 3.3 sigctx（共享包）

- `SignalContext()`：捕获 SIGINT / SIGTERM / SIGQUIT，取消 context 实现优雅退出（SIGHUP 默认排除，注释说明如需配置热加载可自行处理）。
- `NotifyReload()`：预留 SIGHUP 重载通道。

## 四、构建与发布

- `.goreleaser.yaml`（version 2）：两个独立 build（fn-qb-proxy / fn-qb-http），各自打包 tar.gz，命名兼容 `uname`（amd64 → x86_64）；docker buildx 构建 amd64/arm64 镜像并合并 manifest；`ldflags` 注入 `version` / `buildTime`。
- `.github/workflows/release.yml`：打 tag 触发发布。
- `Dockerfile` / `docker-compose.yml`：fn-qb-http 容器化，挂载 `/run/fn-qb-proxy` 只读到 `/app/sockets`，映射 18080 端口。

## 五、潜在问题清单（按严重度排序）

### P1（中高）代理创建失败后无重试 → 代理 Socket 可能永久缺失

`createProxy` 中对 `test -S` 检查新 socket 是否就绪，未就绪直接 `return`，不产生重试。此后若凭据不再变化，就不会再有 add 事件 → 该用户代理永远不会创建。qBittorrent 启动较慢或 socket 延迟创建时会踩中。

**建议**：在扫描循环中对"凭据存在但代理未创建"的用户补发 add 事件，或将创建失败记录为待重试状态。

### P2（中高）remove→add 快速切换存在竞态，旧 goroutine 可能误删新 server 映射

`createProxy` 中 `srv.Serve()` 结束后有一个 goroutine 无条件执行 `delete(userServers, user)`。若 remove（旧 server Shutdown）与 add（新 server 入 map）交错执行，旧 goroutine 会误删新 server 的引用，导致后续 remove 找不到 server、重复创建 listener。

**建议**：goroutine 中删除前校验映射中的 server 是否仍是自己：`if s, ok := userServers[user]; ok && s == srv { delete(...) }`。

### P3（中）事件通道满时静默丢事件

`userEvents` 容量仅 10 且非阻塞发送，多用户同时变更时可能丢 add/remove 事件，代理状态与凭据不一致且无自愈。

**建议**：改为带重试的发送，或定期对账（期望状态 vs 实际代理状态）。

### P4（中）fn-qb-http 收到终止信号后进程不会退出

`httpCmd` 中 `ListenAndServe()` 不会因 ctx 取消而返回，`<-ctx.Done()` 永远走不到，也没有 `server.Shutdown()`。Ctrl+C / SIGTERM 后进程会挂住，只能靠 systemd/kill -9 兜底。

**建议**：监听 ctx，取消时调用 `server.Shutdown(ctx)` 并等待完成。

### P5（低）代码卫生

- `http/main.go` 的 `PasswordNomatch` header 在该组件内从未被消费也未删除，逻辑易混淆。
- `http.Server` 未设置 `ReadTimeout` / `IdleTimeout`，存在慢速攻击面。
- `body, _ := io.ReadAll(r.In.Body)` 忽略错误。
- `proxy/proxy.go` 中未提交的"改动"仅是行尾多加了一个空格，实质无变化。

### P6（低）工作区卫生

- 行尾混乱：系统 git 显示只有 `README.md` 与 `proxy/proxy.go` 有真实改动，其余大量 "M" 是 CRLF/LF 差异噪音。
- `.claude/settings.local.json` 被以 URL 编码形式保存（内容应为 `{"permissions":{"allow":["Bash(go build:*)"]}}`），格式损坏，建议修复或删除。

## 六、整体评价

架构清晰、职责单一，两个组件解耦良好；事件驱动的用户代理管理思路正确；防改密、防退出等安全拦截考虑到了实际运维场景。主要短板集中在**并发竞态与失败自愈**（P1/P2/P3）和 **fn-qb-http 退出逻辑**（P4），建议优先修复。

## 七、当前 git 状态（分析时点）

- 分支：main；HEAD：`2c54ae7a` feat: 添加 ARM64 架构构建支持。
- 真实未提交改动：
  - `README.md`：文档更新（v0.2.0 独立打包说明、fn-qb-http 参数表等）。
  - `proxy/proxy.go`：1 行行尾空格差异，无功能影响。
  - 未跟踪 `.claude/`（含编码损坏的 `settings.local.json`）。
