package main

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

type UserCredentials struct {
	Password string // 密码
	SockPath string // Socket文件位置
}

var credentials = make(map[string]UserCredentials) // 用户名 -> 凭据信息

func doFindQbUser() error {
	// 使用pgrep查找所有匹配的进程ID
	pgrepCmd := exec.Command("pgrep", "-f", "qbittorrent-nox")
	pidOutput, err := pgrepCmd.Output()
	if err != nil {
		return fmt.Errorf("qbittorrent-nox process not found: %w", err)
	}

	// 分割多个PID（每行一个PID）
	pids := strings.Fields(string(pidOutput))
	if len(pids) == 0 {
		return fmt.Errorf("no qbittorrent-nox process found")
	}

	// 保存旧凭据用于比较变更
	oldCredentials := make(map[string]UserCredentials)
	for k, v := range credentials {
		oldCredentials[k] = v
	}
	// 创建新凭据映射
	newCredentials := make(map[string]UserCredentials)
	found := false

	// 遍历每个PID查找有效的用户名、密码和Socket路径
	for _, pid := range pids {
		// 获取单个进程的完整命令行
		psCmd := exec.Command("ps", "-p", pid, "-o", "command=")
		output, err := psCmd.Output()
		if err != nil {
			logrus.Errorf("failed to get command line for PID %s: %v", pid, err)
			continue
		}
		cmdLine := string(output)

		// 1. 提取用户名（从--profile=/home/用户名/...）
		userRe := regexp.MustCompile(`--profile=/home/([^/]+)/`)
		userMatches := userRe.FindStringSubmatch(cmdLine)
		if len(userMatches) < 2 {
			logrus.Warnf("PID %s: no valid --profile found (expected /home/username/...)", pid)
			continue
		}
		username := userMatches[1]

		// 2. 提取密码（--webui-password=xxx）
		pwdRe := regexp.MustCompile(`--webui-password=(\S+)`)
		pwdMatches := pwdRe.FindStringSubmatch(cmdLine)
		if len(pwdMatches) < 2 {
			logrus.Warnf("PID %s: no --webui-password found", pid)
			continue
		}
		pwd := pwdMatches[1]

		// 3. 提取Socket文件位置（--webui-sock-path=xxx）
		sockRe := regexp.MustCompile(`--webui-sock-path=(\S+)`)
		sockMatches := sockRe.FindStringSubmatch(cmdLine)
		sockPath := ""
		if len(sockMatches) >= 2 {
			sockPath = sockMatches[1]
		} else {
			logrus.Warnf("PID %s: no --webui-sock-path found", pid)
		}

		// 存储用户名、密码和Socket路径对应关系到新凭据映射
		newCredentials[username] = UserCredentials{
			Password: pwd,
			SockPath: sockPath,
		}
		logrus.Debugf("Extracted credentials for user: %s (PID: %s, Socket: %s)", username, pid, sockPath)
		found = true
	}

	// 确定新增和删除的用户并发送事件
	// 处理已删除的用户
	for username := range oldCredentials {
		if _, exists := newCredentials[username]; !exists {
			logrus.Debugf("User %s removed, sending remove event", username)
			userEvents <- UserEvent{EventType: "remove", Username: username}
		}
	}

	// 处理新增的用户
	for username := range newCredentials {
		if _, exists := oldCredentials[username]; !exists {
			logrus.Debugf("User %s added, sending add event", username)
			userEvents <- UserEvent{EventType: "add", Username: username}
		}
	}

	// 更新凭据
	credentials = newCredentials

	if !found {
		return fmt.Errorf("no valid qbittorrent-nox processes with required parameters found")
	}
	return nil
}

func findQbUser(ctx context.Context) {
	logrus.Info("Starting qb user finder...")

	// 立即执行一次密码获取
	if err := doFindQbUser(); err != nil {
		logrus.Errorf("Failed to fetch qb password: %v", err)
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	// 主循环，使用context处理终止信号
	for {
		select {
		case <-ticker.C:
			if err := doFindQbUser(); err != nil {
				logrus.Errorf("Failed to fetch qb password: %v", err)
			}
		case <-ctx.Done():
			logrus.Info("Context cancelled, exiting...")
			return
		}
	}
}
