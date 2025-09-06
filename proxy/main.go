package main

import (
	"os"

	"github.com/leganck/fn-qb-proxy/sigctx"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

const SOCKET_SUBDIR = "socket-subdir"
const DEBUG = "debug"

var socketSubDir string

// 处理 Unix Socket 连接
func proxySocket(ctlCtx *cli.Context) error {
	// 根据flag设置日志级别
	if ctlCtx.Bool(DEBUG) {
		logrus.SetLevel(logrus.DebugLevel)
	}

	socketSubDir = ctlCtx.String(SOCKET_SUBDIR)
	if socketSubDir != "" {
		logrus.Infof("Socket subdirectory enabled: %s", socketSubDir)
	}
	// 创建带取消功能的上下文来处理终止信号
	ctx, cancel := sigctx.SignalContext()
	defer cancel()

	// 启动查找qb密码的goroutine
	go findQbUser(ctx)

	// 启动HTTP服务器
	return startHTTPServer(ctx)
}

func main() {
	// 配置logrus为systemd兼容格式：无颜色、ISO 8601时间戳
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02T15:04:05Z07:00", // ISO 8601标准时间格式
		ForceColors:     false,                       // 禁用颜色输出（systemd日志不需要）
		DisableQuote:    true,                        // 禁用字符串自动加引号
	})
	logrus.SetLevel(logrus.InfoLevel)

	app := &cli.App{
		Name:   "fn-qb-proxy",
		Usage:  "fn-qb-proxy is a find proxy for qBittorrent in fnOS",
		Action: proxySocket,
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    DEBUG,
				Usage:   "enable debug logging",
				Aliases: []string{"d"},
			},
			&cli.StringFlag{
				Name:    SOCKET_SUBDIR,
				Usage:   "Subdirectory to append to socket path (optional)",
				Value:   "",
				Aliases: []string{"ss"},
			},
		},
		// 添加service子命令
		Commands: []*cli.Command{
			{
				Name:  "service",
				Usage: "Manage system service",
				Subcommands: []*cli.Command{
					{
						Name:   "install",
						Usage:  "Install as systemd service",
						Action: installService,
					},
					{
						Name:   "uninstall",
						Usage:  "Uninstall systemd service",
						Action: uninstallService,
					},
					{
						Name:   "start",
						Usage:  "Start systemd service",
						Action: startService,
					},
					{
						Name:   "stop",
						Usage:  "Stop systemd service",
						Action: stopService,
					},
					{
						Name:   "restart",
						Usage:  "Restart systemd service",
						Action: restartService,
					},
				},
			},
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		logrus.Fatal(err)
	}
}
