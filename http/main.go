package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"strings"

	"github.com/leganck/fn-qb-proxy/sigctx"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

const (
	qbtSocketPath = "/home/admin/qbt.sock"
	loginAPIPath  = "/api/v2/auth/login"
)

func httpCmd(cliCtx *cli.Context) error {
	uds := cliCtx.String("uds")
	port := cliCtx.Int("port")
	authPassword := cliCtx.String("password")
	debug := cliCtx.Bool("debug")

	// 新增：根据debug标志设置logrus日志级别
	if debug {
		logrus.SetLevel(logrus.DebugLevel)
	}

	ctx, cancel := sigctx.SignalContext()
	defer cancel()

	// 替换：使用logrus.Info输出服务启动信息
	logrus.Infof("http running on port %d", port)

	proxy := proxy(uds, authPassword)
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: &proxy,
		BaseContext: func(listener net.Listener) context.Context {
			return ctx
		},
	}
	err := server.ListenAndServe()
	if err != nil {
		return fmt.Errorf("listen and serve: %w", err)
	}
	<-ctx.Done()

	return nil
}

func proxy(uds string, authPassword string) httputil.ReverseProxy {

	proxy := httputil.ReverseProxy{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				// 检查目标socket是否存在
				if _, err := os.Stat(uds); os.IsNotExist(err) {
					logrus.Warnf("Target socket does not exist: %s", uds)
					return nil, fmt.Errorf("target socket does not exist: %s", uds)
				}
				conn, err := net.Dial("unix", uds)
				if err != nil {
					logrus.Errorf("Failed to dial target socket %s: %v", uds, err)
				}
				return conn, err

			},
		},
		Rewrite: func(r *httputil.ProxyRequest) {
			logrus.Debugf("request: %v", r.In.URL.Path)
			r.Out.URL.Scheme = "http"
			r.Out.URL.Host = fmt.Sprintf("unix://%s", uds)
			r.Out.Host = fmt.Sprintf("unix://%s", uds)

			body, _ := io.ReadAll(r.In.Body)
			err := r.In.ParseForm()
			if err != nil {
				logrus.Errorf("parse form err: %v", err)
			}

			if strings.Contains(r.In.URL.Path, loginAPIPath) {
				if authPassword != "" {
					password := authPassword

					parts := strings.Split(string(body), "&")
					logrus.Debugf("login form parts: %v", parts)
					for _, part := range parts {
						if strings.HasPrefix(part, "password=") {
							formPassword := strings.TrimPrefix(part, "password=")
							if formPassword != authPassword {
								r.Out.Header.Set("PasswordNomatch", "true")
								if formPassword != "" {
									password = formPassword
								}
							}
						}
					}
					body = []byte(fmt.Sprintf("username=admin&password=%s", password))
				}

				r.Out.Header.Set("Content-Length", fmt.Sprintf("%d", len(body)))
				r.Out.ContentLength = int64(len(body))
			}

			r.Out.Header.Del("Referer")
			r.Out.Header.Del("Origin")
			r.Out.Body = io.NopCloser(bytes.NewBuffer(body))
		},
	}
	return proxy
}

func main() {
	logrus.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: "2006-01-02 15:04:05", // 保留原有时间格式
		FieldMap: logrus.FieldMap{
			logrus.FieldKeyTime:  "timestamp", // 标准时间字段名
			logrus.FieldKeyLevel: "level",     // 日志级别字段名
			logrus.FieldKeyMsg:   "message",   // 日志内容字段名
		},
	})
	logrus.SetLevel(logrus.InfoLevel)

	app := &cli.App{
		Name:   "fn-qb-http",
		Usage:  "fn-qb-http is a http for qBittorrent in fnOS",
		Action: httpCmd,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "uds",
				Usage:   "qBittorrent unix domain socket(uds) path",
				Value:   qbtSocketPath,
				EnvVars: []string{"UDS"},
			},
			&cli.BoolFlag{
				Name:    "debug",
				Aliases: []string{"d"},
				Value:   false,
				EnvVars: []string{"DEBUG"},
			},
			&cli.IntFlag{
				Name:    "port",
				Aliases: []string{"p"},
				Usage:   "http running port",
				Value:   18080,
				EnvVars: []string{"PORT"},
			},
			&cli.StringFlag{
				Name:    "password",
				Usage:   "if not set, any password will be accepted",
				Value:   "",
				EnvVars: []string{"PASSWORD"},
			},
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}
