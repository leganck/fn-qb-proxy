# fn-qb-proxy

## What is it?

基于 [fnos-qb-proxy](https://github.com/xxxuuu/fnos-qb-proxy) 修改的 qBittorrent 代理工具集，包含两个核心组件：

- **fn-qb-proxy**: 后台服务，自动检测 fnOS 中的 qBittorrent 进程，提取动态密码与 Unix Socket 路径，为每个用户创建独立的代理
  Socket。
- **fn-qb-http**: HTTP 服务器，将 Unix Socket 代理转换为 HTTP 服务，支持外部应用（如 MoviePilot、NasTools）通过 HTTP 访问
  qBittorrent。

解决 fnOS 中 qBittorrent WebUI 默认关闭、动态密码及 Unix Socket 访问限制问题，同时不影响系统自带下载器运行。

## fn-qb-proxy

### 下载与安装

从 [Releases](https://github.com/leganck/fn-qb-proxy/releases) 下载对应架构的两个压缩包（fn-qb-proxy 与 fn-qb-http 独立打包，均支持
Linux amd64 / arm64）：

```shell
# 下载并解压 fn-qb-proxy
wget https://github.com/leganck/fn-qb-proxy/releases/download/v0.2.0/fn-qb-proxy_Linux_x86_64.tar.gz && tar -zxvf fn-qb-proxy_Linux_x86_64.tar.gz

# 赋予执行权限
chmod +x fn-qb-proxy
```

```shell
$ ./fn-qb-proxy -h
NAME:
   fn-qb-proxy - fn-qb-proxy is a find proxy for qBittorrent in fnOS

USAGE:
   fn-qb-proxy [global options] command [command options]

COMMANDS:
   service   Manage system service
   help, h   Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --debug, -d                     enable debug logging (default: false)
   --socket-perm value, -sp value  Socket file permissions in octal format (default: 0660)
   --proxy-socket-dir value, -psd value  Directory for storing proxy socket files (default: "/run/fn-qb-proxy") [$PROXY_SOCKET_DIR]
   --help, -h                      show help
```

运行后

```shell
# 基本运行（默认配置）
./fn-qb-proxy

# 调试模式 + 自定义 socket 存放目录
./fn-qb-proxy -d -psd /run/fn-qb-proxy
```

> 提示：`--proxy-socket-dir` 也可通过环境变量 `PROXY_SOCKET_DIR` 指定。

### 配置系统服务

上面的命令会一直在前台运行，可以使用 Systemd 配置成 daemon 在后台自动运行

#### 安装服务（自动复制二进制到 /usr/local/bin 并配置 systemd）

```shell
sudo ./fn-qb-proxy service install
```

#### 启动服务

```shell
sudo ./fn-qb-proxy service start
```

#### 查看状态

```shell
sudo systemctl status fn-qb-proxy
```

```bash
$ sudo systemctl status fn-qb-proxy
● fn-qb-proxy.service - fnOS qBittorrent Password Service
     Loaded: loaded (/etc/systemd/system/fn-qb-proxy.service; enabled; preset: enabled)
     Active: active (running) since Fri 2025-04-18 14:37:48 CST; 15s ago
   Main PID: 1325579 (fn-qb-proxy)
      Tasks: 9 (limit: 77001)
     Memory: 4.8M
        CPU: 765ms
     CGroup: /system.slice/fn-qb-proxy.service
             └─1325579 /usr/local/bin/fn-qb-proxy -psd /home/admin/qb-proxy

Apr 18 14:37:48 fn systemd[1]: Started fn-qb-proxy.service - fnOS qBittorrent Password Service.
Apr 18 14:37:48 fn fn-qb-proxy[1325579]: 2025/04/18 14:37:48 Starting qb password finder...
```

## fn-qb-http（HTTP 访问服务）

### 核心功能

fn-qb-http 服务主要实现两类 Unix Socket 到 HTTP 服务的转换，具体逻辑如下：

1. **对接 fn-qb-proxy 生成的 Unix Socket**
    - 密码校验规则：
        - 当配置中 `password` 字段为空时，采用“宽松校验”，输入任意密码均可完成登录；
        - 当配置中 `password` 字段不为空时，采用“严格校验”，仅输入与 `password` 配置值完全一致的密码可登录。

2. **直接对接 qBittorrent 进程原生 Unix Socket**
    - 无需额外配置 `password`，服务会自动使用 qBittorrent 进程启动时动态生成的随机密码进行鉴权；
    - 可通过以下 Shell 命令查询当前运行的 qBittorrent 进程：
      ```shell
      ps aux | grep [q]bittorrent-nox
      ```

### 参数与环境变量

fn-qb-http 支持以下命令行参数（均可通过环境变量配置，Docker 部署时推荐使用环境变量）：

| 参数 | 别名 | 默认值 | 环境变量 | 说明 |
| ---- | ---- | ------ | -------- | ---- |
| `--uds` | - | qBittorrent 原生 UDS 路径 | `UDS` | 要代理的 Unix Socket 路径 |
| `--port` | `-p` | `18080` | `PORT` | HTTP 监听端口 |
| `--password` | - | 空 | `PASSWORD` | 登录密码；为空时接受任意密码（宽松校验） |
| `--debug` | `-d` | `false` | `DEBUG` | 开启调试日志 |

### 部署方式

部署配置可直接参考项目内的 [docker-compose.yml](docker-compose.yml) 文件，按文件内的示例配置进行环境搭建即可。
