package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

const serviceName = "fn-qb-proxy.service"
const servicePath = "/etc/systemd/system/" + serviceName
const systemdTemplate = `[Unit]
Description=fn-qb-proxy service
After=dlcenter.service

[Service]
Type=simple
ExecStart=%s  -d #可执行文件
Restart=always
RestartSec=5
User=root
Group=root
Environment="PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

[Install]
WantedBy=multi-user.target
`

func installService(c *cli.Context) error {
	// 获取当前可执行文件路径
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %v", err)
	}
	exePath, err = filepath.Abs(exePath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %v", err)
	}

	// 新增：获取可执行文件名并定义目标路径
	exeName := filepath.Base(exePath)
	targetPath := filepath.Join("/usr/local/bin", exeName)

	// 新增：复制可执行文件到/usr/local/bin
	srcFile, err := os.Open(exePath)
	if err != nil {
		return fmt.Errorf("failed to open source executable: %v", err)
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return fmt.Errorf("failed to create target file %s: %v (try with sudo)", targetPath, err)
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("failed to copy executable to %s: %v (try with sudo)", targetPath, err)
	}
	logrus.Infof("Executable copied to %s", targetPath)

	// 构建systemd服务文件内容（使用复制后的路径）

	serviceContent := fmt.Sprintf(systemdTemplate, targetPath)

	// 写入服务文件
	if err := os.WriteFile(servicePath, []byte(serviceContent), 0644); err != nil {
		return fmt.Errorf("failed to write service file: %v (try with sudo)", err)
	}

	// 重新加载systemd配置
	if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("failed to reload systemd: %v (try with sudo)", err)
	}

	// 设置开机自启
	if err := exec.Command("systemctl", "enable", serviceName).Run(); err != nil {
		return fmt.Errorf("failed to enable service: %v (try with sudo)", err)
	}

	logrus.Infof("Service %s installed successfully", serviceName)
	logrus.Info("You can start the service with: sudo systemctl start fn-qb-proxy")
	return nil
}

func uninstallService(c *cli.Context) error {
	// 停止服务
	exec.Command("systemctl", "stop", serviceName).Run()

	// 禁用服务
	exec.Command("systemctl", "disable", serviceName).Run()

	serviceFileContent, err := os.ReadFile(servicePath)
	if err == nil {
		// 编译正则表达式：匹配 ExecStart=... 行，捕获路径（支持空格和注释）
		//  pattern 说明：
		//  ^ExecStart\s*= : 行首匹配 "ExecStart"，允许等号前有空格
		//  \s*(.+?)\s*   : 捕获等号后内容（非贪婪匹配），忽略前后空格
		//  (?:#.*)?$     : 忽略行尾注释（如 # 后的内容）
		execStartRegex := regexp.MustCompile(`ExecStart\s*=\s*(.*?)\s`)

		scanner := bufio.NewScanner(strings.NewReader(string(serviceFileContent)))
		for scanner.Scan() {
			line := scanner.Text() // 保留原始行（不 trim，避免影响注释判断）
			match := execStartRegex.FindStringSubmatch(line)
			if match != nil {
				execPath := match[1] // 提取捕获组中的路径

				// 删除可执行文件
				if err := os.Remove(execPath); err != nil {
					return fmt.Errorf("Failed to remove executable %s: %v", execPath, err)

				} else {
					logrus.Infof("Removed executable %s", execPath)
				}
				break
			}
		}
		if err := scanner.Err(); err != nil {
			logrus.Warnf("Error reading service file: %v", err)
		}
	} else {
		logrus.Warnf("Service file %s not found, skipping executable removal", servicePath)
	}

	// 删除服务文件
	if err := os.Remove(servicePath); err != nil {
		return fmt.Errorf("failed to remove service file: %v (try with sudo)", err)
	}

	// 重新加载systemd配置
	if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("failed to reload systemd: %v (try with sudo)", err)
	}

	logrus.Infof("Service %s uninstalled successfully", serviceName)
	return nil
}

func startService(c *cli.Context) error {
	if err := exec.Command("systemctl", "start", serviceName).Run(); err != nil {
		return fmt.Errorf("failed to start service: %v (try with sudo)", err)
	}
	logrus.Infof("Service %s started successfully", serviceName)
	return nil
}

func stopService(c *cli.Context) error {
	if err := exec.Command("systemctl", "stop", serviceName).Run(); err != nil {
		return fmt.Errorf("failed to stop service: %v (try with sudo)", err)
	}
	logrus.Infof("Service %s stopped successfully", serviceName)
	return nil
}

func restartService(c *cli.Context) error {
	if err := exec.Command("systemctl", "restart", serviceName).Run(); err != nil {
		return fmt.Errorf("failed to restart service: %v (try with sudo)", err)
	}
	logrus.Infof("Service %s restarted successfully", serviceName)
	return nil
}
