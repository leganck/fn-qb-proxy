package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

const loginAPIPath = "/api/v2/auth/login"

// 添加用户服务器映射和同步锁
var (
	userServers = make(map[string]*http.Server)
	serverMutex sync.Mutex
	// 用户变更事件通道
	userEvents = make(chan UserEvent, 10)
)

// UserEvent 用户事件类型
type UserEvent struct {
	EventType string // "add" 或 "remove"
	Username  string
}

func createProxy(username string) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				targetSocket := credentials[username].SockPath
				// 检查目标socket是否存在
				if _, err := os.Stat(targetSocket); os.IsNotExist(err) {
					logrus.Warnf("Target socket does not exist: %s", targetSocket)
					return nil, fmt.Errorf("target socket does not exist: %s", targetSocket)
				}
				conn, err := net.Dial("unix", targetSocket)
				if err != nil {
					logrus.Errorf("Failed to dial target socket %s: %v", targetSocket, err)
				}
				return conn, err
			},
		},
		Rewrite: func(r *httputil.ProxyRequest) {
			logrus.Debugf("request: %v,%v\n", r.In.Method, r.In.URL.Path)
			r.Out.URL.Scheme = "http"
			targetSocket := credentials[username].SockPath
			r.Out.Host = fmt.Sprintf("unix://%s", targetSocket)
			r.Out.URL.Host = fmt.Sprintf("unix://%s", targetSocket)

			body, _ := io.ReadAll(r.In.Body)
			err := r.In.ParseForm()

			if err != nil {
				logrus.Errorf("parse form err: %v", err)
			}

			if strings.Contains(r.In.URL.Path, loginAPIPath) {
				password := credentials[username].Password

				passwordNomatch := r.In.Header.Get("PasswordNomatch")
				if passwordNomatch != "" {
					password = ""
				}

				body = []byte(fmt.Sprintf("username=admin&password=%s", password))

				r.Out.Header.Set("Content-Length", fmt.Sprintf("%d", len(body)))
				r.Out.ContentLength = int64(len(body))
			}
			r.Out.Header.Del("PasswordNomatch")
			r.Out.Header.Del("Referer")
			r.Out.Header.Del("Origin")
			r.Out.Body = io.NopCloser(bytes.NewBuffer(body))
		},
	}
}

func startHTTPServer(ctx context.Context) error {
	// 等待第一次获取凭证信息
	for {
		if len(credentials) > 0 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}

	// 启动用户事件监听goroutine
	go listenUserEvents(ctx)

	// 阻塞直到上下文取消
	<-ctx.Done()

	// 清理所有用户代理
	cleanupAllProxies()

	return nil
}

// 监听用户事件并处理
func listenUserEvents(ctx context.Context) {
	for {
		select {
		case event := <-userEvents:
			switch event.EventType {
			case "add":
				createUserProxy(event.Username)
			case "remove":
				removeUserProxy(event.Username)
			}
		case <-ctx.Done():
			return
		}
	}
}

// 创建用户代理
func createUserProxy(username string) {
	cred, exists := credentials[username]
	if !exists || cred.SockPath == "" {
		logrus.Warnf("User %s credentials not found or invalid", username)
		return
	}

	serverMutex.Lock()
	defer serverMutex.Unlock()

	// 检查是否已存在服务器
	if _, exists := userServers[username]; exists {
		logrus.Debugf("Proxy for user %s already exists", username)
		return
	}

	// 创建监听同目录下的新socket
	dir := filepath.Dir(cred.SockPath)
	newSocketPath := filepath.Join(dir, fmt.Sprintf("%s-qb-proxy.sock", username))

	// 删除可能存在的旧socket文件
	os.Remove(newSocketPath)

	// 创建Unix socket监听器
	listener, err := net.Listen("unix", newSocketPath)
	if err != nil {
		logrus.Errorf("Failed to create listener for user %s: %v", username, err)
		return
	}

	// 设置socket权限
	if err := os.Chmod(newSocketPath, 0666); err != nil {
		logrus.Errorf("Failed to set permissions for socket %s: %v", newSocketPath, err)
	}

	// 创建反向代理
	proxy := createProxy(username)

	// 启动服务器
	server := &http.Server{
		Handler: proxy,
	}

	// 保存服务器引用
	userServers[username] = server

	go func(user, sockPath string, srv *http.Server, lst net.Listener) {
		logrus.Infof("Starting HTTP proxy server for user %s on %s", user, sockPath)

		if err := srv.Serve(lst); err != nil && err != http.ErrServerClosed {
			logrus.Errorf("HTTP server error for user %s: %v", user, err)
		}

		// 服务器关闭后清理
		serverMutex.Lock()
		delete(userServers, user)
		serverMutex.Unlock()
		os.Remove(sockPath)
	}(username, newSocketPath, server, listener)

	logrus.Infof("Proxy server created for user %s", username)
}

// 删除用户代理
func removeUserProxy(username string) {
	serverMutex.Lock()
	defer serverMutex.Unlock()

	server, exists := userServers[username]
	if !exists {
		logrus.Debugf("No proxy server found for user %s", username)
		return
	}

	// 关闭服务器
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logrus.Errorf("Failed to shutdown server for user %s: %v", username, err)
		// 强制关闭
		server.Close()
	}

	// 清理socket文件
	cred := credentials[username]
	dir := filepath.Dir(cred.SockPath)
	socketPath := filepath.Join(dir, fmt.Sprintf("%s-qb-proxy.sock", username))
	os.Remove(socketPath)

	// 从映射中删除
	delete(userServers, username)
	logrus.Infof("Proxy server removed for user %s", username)
}

// 清理所有用户代理
func cleanupAllProxies() {
	serverMutex.Lock()
	defer serverMutex.Unlock()

	for username, server := range userServers {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		server.Shutdown(ctx)
		cancel()

		cred := credentials[username]
		dir := filepath.Dir(cred.SockPath)
		socketPath := filepath.Join(dir, fmt.Sprintf("%s-qb-proxy.sock", username))
		os.Remove(socketPath)

		delete(userServers, username)
	}
}
