package main

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

type UserCredentials struct {
	Password string // 密码
	SockPath string // Socket文件位置
}

var (
	credentials = make(map[string]UserCredentials)
	credsMutex  sync.RWMutex
)

var (
	pwdRe  = regexp.MustCompile(`--webui-password=(\S+)`)
	sockRe = regexp.MustCompile(`--webui-sock-path=(\S+)`)
)

func doFindQbUser() error {
	// 使用pgrep查找所有匹配的进程ID
	pgrepCmd := exec.Command("pgrep", "-f", "qbittorrent-nox")
	pidOutput, err := pgrepCmd.Output()
	if err != nil {
		// pgrep 找不到进程时返回 exit status 1，这是正常情况
		// 只有其他错误（如命令不存在）才应该报错
		if strings.Contains(err.Error(), "exit status 1") {
			return nil // 进程未找到，不是错误
		}
		return fmt.Errorf("pgrep command failed: %w", err)
	}

	pids := strings.Fields(string(pidOutput))
	if len(pids) == 0 {
		return nil // 无进程，正常情况
	}

	credsMutex.RLock()
	oldCredentials := make(map[string]UserCredentials)
	for k, v := range credentials {
		oldCredentials[k] = v
	}
	credsMutex.RUnlock()
	// 创建新凭据映射
	newCredentials := make(map[string]UserCredentials)
	found := false

	// 遍历每个PID查找有效的用户名、密码和Socket路径
	for _, pid := range pids {
		// 获取用户名
		userCmd := exec.Command("ps", "-o", "user=", "-p", pid)
		userOutput, err := userCmd.Output()
		if err != nil {
			logrus.Errorf("failed to get user for PID %s: %v", pid, err)
			continue
		}
		username := strings.TrimSpace(string(userOutput))
		if username == "" {
			logrus.Warnf("PID %s: empty username", pid)
			continue
		}

		// 获取命令行
		psCmd := exec.Command("ps", "-p", pid, "-o", "command=")
		psOutput, err := psCmd.Output()
		if err != nil {
			logrus.Errorf("failed to get command line for PID %s: %v", pid, err)
			continue
		}
		cmdLine := strings.TrimSpace(string(psOutput))
		if cmdLine == "" {
			logrus.Debugf("PID %s: empty command line", pid)
			continue
		}

		pwdMatches := pwdRe.FindStringSubmatch(cmdLine)
		if len(pwdMatches) < 2 {
			logrus.Warnf("PID %s: no --webui-password found", pid)
			continue
		}
		pwd := pwdMatches[1]

		sockMatches := sockRe.FindStringSubmatch(cmdLine)
		if len(sockMatches) < 2 {
			logrus.Warnf("PID %s: no --webui-sock-path found", pid)
			continue
		}
		sockPath := sockMatches[1]

		newCredentials[username] = UserCredentials{
			Password: pwd,
			SockPath: sockPath,
		}
		logrus.Debugf("Extracted credentials for user: %s (PID: %s, Socket: %s)", username, pid, sockPath)
		found = true
	}

	for username, oldCred := range oldCredentials {
		newCred, exists := newCredentials[username]
		if !exists {
			logrus.Debugf("User %s removed, sending remove event", username)
			sendUserEvent(eventTypeRemove, username)
		} else if oldCred != newCred {
			// 检查新的 sock 文件是否存在
			if _, err := exec.Command("test", "-S", newCred.SockPath).CombinedOutput(); err != nil {
				logrus.Debugf("User %s credentials changed but new sock %s not ready, only removing", username, newCred.SockPath)
				sendUserEvent(eventTypeRemove, username)
			} else {
				logrus.Debugf("User %s credentials changed, removing and re-adding", username)
				sendUserEvent(eventTypeRemove, username)
				sendUserEvent(eventTypeAdd, username)
			}
		}
	}

	for username := range newCredentials {
		if _, exists := oldCredentials[username]; !exists {
			logrus.Debugf("User %s added, sending add event", username)
			sendUserEvent(eventTypeAdd, username)
		}
	}

	credsMutex.Lock()
	credentials = newCredentials
	credsMutex.Unlock()

	if !found {
		return fmt.Errorf("no valid qbittorrent-nox processes with required parameters found")
	}
	return nil
}

func findQbUser(ctx context.Context) {
	logrus.Info("Starting qb user finder...")

	// 立即执行一次密码获取
	if err := doFindQbUser(); err != nil {
		logrus.Errorf("Failed to fetch qb credentials: %v", err)
	} else {
		credsMutex.RLock()
		count := len(credentials)
		credsMutex.RUnlock()
		if count == 0 {
			logrus.Info("No qbittorrent-nox processes found (will retry in 5s)")
		}
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	// 主循环，使用context处理终止信号
	for {
		select {
		case <-ticker.C:
			if err := doFindQbUser(); err != nil {
				logrus.Errorf("Failed to fetch qb credentials: %v", err)
			}
		case <-ctx.Done():
			logrus.Info("Context cancelled, exiting...")
			return
		}
	}
}
