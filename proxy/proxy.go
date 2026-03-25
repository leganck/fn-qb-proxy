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
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	loginAPIPath         = "/api/v2/auth/login"
	logoutAPIPath        = "/api/v2/auth/logout"
	setPreferencesAPIPath = "/api/v2/app/setPreferences"
	eventTypeAdd         = "add"
	eventTypeRemove      = "remove"
)

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

// sendUserEvent 发送用户变更事件（非阻塞）
func sendUserEvent(eventType, username string) {
	select {
	case userEvents <- UserEvent{EventType: eventType, Username: username}:
		logrus.Debugf("Sent %s event for user %s", eventType, username)
	default:
		logrus.Warnf("Failed to send %s event for user %s: channel full", eventType, username)
	}
}

func getProxySocketPath(username string) string {
	return fmt.Sprintf("%s/%s-qb-proxy.sock", proxySocketDir, username)
}

func createProxy(username string) *httputil.ReverseProxy {
	credsMutex.RLock()
	cred := credentials[username]
	credsMutex.RUnlock()

	return &httputil.ReverseProxy{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				conn, err := net.Dial("unix", cred.SockPath)
				if err != nil {
					logrus.Errorf("Failed to dial target socket %s: %v", cred.SockPath, err)
				}
				return conn, err
			},
		},
		Rewrite: func(r *httputil.ProxyRequest) {
			logrus.Debugf("request: %v,%v", r.In.Method, r.In.URL.Path)
			r.Out.URL.Scheme = "http"
			r.Out.Host = fmt.Sprintf("unix://%s", cred.SockPath)
			r.Out.URL.Host = fmt.Sprintf("unix://%s", cred.SockPath)

			body, err := io.ReadAll(r.In.Body)
			if err != nil {
				logrus.Errorf("Failed to read request body: %v", err)
				return
			}

			if err := r.In.ParseForm(); err != nil {
				logrus.Errorf("Failed to parse form: %v", err)
			}

			if strings.Contains(r.In.URL.Path, loginAPIPath) {
				password := cred.Password

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

// createProxyHandler 创建带拦截功能的 HTTP Handler
// 拦截退出请求和修改用户名密码的请求，其余请求转发给反向代理
func createProxyHandler(username string, proxy *httputil.ReverseProxy) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// 拦截退出请求：代理自动注入密码登录，退出无意义
		if strings.Contains(path, logoutAPIPath) {
			logrus.Debugf("Intercepted logout request for user %s", username)
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "Ok.")
			return
		}

		// 拦截修改用户名/密码请求：防止破坏代理的自动登录功能
		if strings.Contains(path, setPreferencesAPIPath) {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "Failed to read request", http.StatusBadRequest)
				return
			}

			if strings.Contains(string(body), "web_ui_password") ||
				strings.Contains(string(body), "web_ui_username") {
				logrus.Warnf("Blocked password/username change attempt for user %s", username)
				http.Error(w, "Changing username/password is not allowed through proxy", http.StatusForbidden)
				return
			}

			// 不包含密码/用户名修改，放行但需恢复 body
			r.Body = io.NopCloser(bytes.NewBuffer(body))
			r.ContentLength = int64(len(body))
		}

		proxy.ServeHTTP(w, r)
	})
}

func startHTTPServer(ctx context.Context) error {
	// 使用 WaitGroup 确保 goroutine 优雅退出
	var wg sync.WaitGroup
	wg.Add(1)

	// 启动用户事件监听goroutine
	go func() {
		defer wg.Done()
		listenUserEvents(ctx)
	}()

	// 阻塞直到上下文取消
	<-ctx.Done()

	// 清理所有用户代理
	cleanupAllProxies()

	// 等待事件监听 goroutine 退出
	wg.Wait()

	return nil
}

// 监听用户事件并处理
func listenUserEvents(ctx context.Context) {
	for {
		select {
		case event := <-userEvents:
			switch event.EventType {
			case eventTypeAdd:
				createUserProxy(event.Username)
			case eventTypeRemove:
				removeUserProxy(event.Username)
			}
		case <-ctx.Done():
			return
		}
	}
}

// 创建用户代理
func createUserProxy(username string) {
	// 先检查凭据是否有效
	credsMutex.RLock()
	cred, exists := credentials[username]
	credsMutex.RUnlock()

	if !exists || cred.SockPath == "" {
		logrus.Warnf("User %s credentials not found or invalid", username)
		return
	}

	// 检查目标 qBittorrent socket 是否已准备好
	if _, err := exec.Command("test", "-S", cred.SockPath).CombinedOutput(); err != nil {
		logrus.Warnf("Target socket %s not ready for user %s, will retry on next scan", cred.SockPath, username)
		return
	}

	serverMutex.Lock()
	defer serverMutex.Unlock()

	// 检查是否已存在服务器
	if _, exists := userServers[username]; exists {
		logrus.Debugf("Proxy for user %s already exists", username)
		return
	}

	newSocketPath := getProxySocketPath(username)

	if err := os.Remove(newSocketPath); err != nil && !os.IsNotExist(err) {
		logrus.Debugf("Failed to remove old socket file %s: %v", newSocketPath, err)
	}

	listener, err := net.Listen("unix", newSocketPath)
	if err != nil {
		logrus.Errorf("Failed to create listener for user %s: %v", username, err)
		return
	}

	if err := os.Chmod(newSocketPath, socketPerm); err != nil {
		logrus.Errorf("Failed to set permissions for socket %s: %v", newSocketPath, err)
		listener.Close()
		return
	}

	// 创建反向代理
	proxy := createProxy(username)

	// 启动服务器，使用拦截器包装
	server := &http.Server{
		Handler: createProxyHandler(username, proxy),
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

	socketPath := getProxySocketPath(username)
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		logrus.Debugf("Failed to remove socket file %s: %v", socketPath, err)
	}

	delete(userServers, username)
	logrus.Infof("Proxy server removed for user %s", username)
}

func cleanupAllProxies() {
	serverMutex.Lock()
	defer serverMutex.Unlock()

	credsMutex.RLock()
	for username, server := range userServers {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := server.Shutdown(ctx); err != nil {
			logrus.Errorf("Failed to shutdown server for user %s: %v", username, err)
			server.Close()
		}
		cancel()

		socketPath := getProxySocketPath(username)
		if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
			logrus.Debugf("Failed to remove socket file %s: %v", socketPath, err)
		}

		delete(userServers, username)
	}
	credsMutex.RUnlock()
}
