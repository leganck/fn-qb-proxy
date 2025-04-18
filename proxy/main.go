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
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/urfave/cli/v2"
)

const (
	pwFile        = "/home/admin/qb-pwd"
	qbtSocketPath = "/home/admin/qbt.sock"
	loginAPIPath  = "/api/v2/auth/login"
)

func watchQbPassword(ctx context.Context, updatePwd func(string), filePath string) {

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("watchQbPassword stopped by context: %v", ctx.Err())
			return
		case <-ticker.C:
			content, err := os.ReadFile(filePath)
			if err != nil {
				// 区分文件不存在和临时性错误
				if os.IsNotExist(err) {
					log.Printf("File does not exist: %v", filePath)
					continue
				}
				log.Printf("Error reading file (%v): %v", filePath, err)
				continue
			}

			if len(content) > 0 {
				newPwd := string(content)
				updatePwd(newPwd)
			}
		}
	}
}

func watchForShutdown() context.Context {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
	ctx, cancelFunc := context.WithCancel(context.Background())
	go func() {
		<-signalChan
		cancelFunc()
	}()
	return ctx
}
func proxyCmd(ctx *cli.Context) error {
	uds := ctx.String("uds")
	port := ctx.Int("port")
	expectedPassword := ctx.String("password")
	passwdFile := ctx.String("pf")
	debug := ctx.Bool("debug")

	// 校验输入参数
	if uds == "" || passwdFile == "" {
		return fmt.Errorf("missing required parameters: uds=%q, passwdFile=%q", uds, passwdFile)
	}

	cancelCtx := watchForShutdown()

	password := ""
	// 安全地更新密码
	go watchQbPassword(cancelCtx, func(newPassword string) {
		if newPassword != password {
			password = newPassword
			log.Printf("password updated: %s", newPassword)
		}
	}, passwdFile)

	log.Printf("proxy running on port %d\n", port)

	proxy := proxy(debug, uds, password, expectedPassword)
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: &proxy,
		BaseContext: func(listener net.Listener) context.Context {
			return cancelCtx
		},
	}
	err := server.ListenAndServe()
	if err != nil {
		return fmt.Errorf("listen and serve: %w", err)
	}
	return nil
}

func proxy(debug bool, uds string, password string, expectedPassword string) httputil.ReverseProxy {
	debugPrint := func(format string, args ...any) {

		if debug {
			log.Printf(format, args...)
		}
	}

	proxy := httputil.ReverseProxy{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return net.Dial("unix", uds)
			},
		},
		Rewrite: func(r *httputil.ProxyRequest) {
			debugPrint("request: %v\n", r.In.URL.Path)
			r.Out.URL.Scheme = "http"
			r.Out.URL.Host = fmt.Sprintf("file://%s", uds)
			r.Out.Host = fmt.Sprintf("file://%s", uds)

			body, _ := io.ReadAll(r.In.Body)
			r.In.ParseForm()
			if strings.Contains(r.In.URL.Path, loginAPIPath) {
				outPassword := password
				if expectedPassword != "" {
					parts := strings.Split(string(body), "&")
					debugPrint("parts: %v\n", parts)
					for _, part := range parts {
						if strings.HasPrefix(part, "password=") {
							inputPassword := strings.TrimPrefix(part, "password=")
							if inputPassword != expectedPassword {
								outPassword = ""
								break
							}
						}
					}
				}

				body = []byte(fmt.Sprintf("username=admin&password=%s", outPassword))
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
	app := &cli.App{
		Name:   "fn-qb-proxy",
		Usage:  "fn-qb-proxy is a proxy for qBittorrent in fnOS",
		Action: proxyCmd,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "pf",
				Usage:   "if not set, any pwd will be accepted",
				Value:   pwFile,
				EnvVars: []string{"PWD-FILE"},
			},
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
				Usage:   "proxy running port",
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
