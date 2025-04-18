# fn-qb-proxy

## What is it?

参考项目[fnos-qb-proxy](https://github.com/xxxuuu/fnos-qb-proxy)修改而来，拆分为两个程序，一个基于容器代理，一个用于获取密码

fnOS 中自带了一个下载器（基于 qBittorrent 和 Aria2），但默认关闭了 WebUI，且采用动态密码。这使得我们无法在外部连接 fnOS 中的
qBittorrent（e.g. 接入 MoviePilot 或 NasTools 等）

该项目是一个简单的代理，能绕过这些限制，提供在外部访问 fnOS 的 qBittorrent 的能力同时不影响 fnOS 自身的下载器运行

## Get Started

### Manual Install

下载 binary 到 fnOS 节点上

```bash
$  wget https://github.com/leganck/fn-qb-proxy/releases/download/v0.1.0/fn-qb-pwd_Linux_x86_64.tar.gz && tar -zxvf fn-qb-pwd_Linux_x86_64.tar.gz
$ chmod +x fn-qb-pwd
```

参数，使用 `--file ` 指定 qBittorrent 密码的输出位置

```bash
$ fn-qb-pwd -h
NAME:
   fnos-qb-pwd - fnos-qb-pwd is a find pwd for qBittorrent in fnOS

USAGE:
   fnos-qb-pwd [global options] command [command options]

COMMANDS:
   help, h  Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --file value, -f value  pwd file path (default: "/home/admin/qb-pwd")
   --help, -h              show help
```

运行后

```bash
$ ./fn-qb-pwd  -f ./qb-pwd
2025/04/18 14:29:34 Starting qb password finder...
```

### Configure Systemd Service

上面的命令会一直在前台运行，可以使用 Systemd 配置成 daemon 在后台自动运行

移动 binary 到 `/usr/local/bin`

```bash
$ sudo mv fn-qb-pwb /usr/local/bin/
```

将以下配置写入到 `/etc/systemd/system/fn-qb-pwd.service`，可自行修改命令参数

```
[Unit]
Description=fnOS qBittorrent Password Service
Before=dlcenter.service

[Service]
ExecStart=/usr/bin/fn-qb-pwd -f "/home/admin/qb-pwd"
Restart=always

[Install]
WantedBy=multi-user.target
```

启用服务

```bash
$ sudo systemctl daemon-reload
$ sudo systemctl enable --now fn-qb-pwd
```

查看服务状态，成功运行

```bash
$ sudo systemctl status fn-qb-pwd
● fn-qb-pwd.service - fnOS qBittorrent Password Service
     Loaded: loaded (/etc/systemd/system/fn-qb-pwd.service; enabled; preset: enabled)
     Active: active (running) since Fri 2025-04-18 14:37:48 CST; 15s ago
   Main PID: 1325579 (fn-qb-pwd)
      Tasks: 9 (limit: 77001)
     Memory: 4.8M
        CPU: 765ms
     CGroup: /system.slice/fn-qb-pwd.service
             └─1325579 /usr/local/bin/fn-qb-pwd -f /home/admin/qb-pwd

Apr 18 14:37:48 fn systemd[1]: Started fn-qb-pwd.service - fnOS qBittorrent Password Service.
Apr 18 14:37:48 fn fn-qb-pwd[1325579]: 2025/04/18 14:37:48 Starting qb password finder...
```

### Proxy Install

参考docker-compose文件
