package main

import (
	"fmt"
	"github.com/urfave/cli/v2"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"syscall"
	"time"
)

const pwFile = "/home/admin/qb-pwd"

// 安全地获取 qBittorrent 的密码
func fetchQbPassword() (string, error) {
	cmd := exec.Command("bash", "-c", "ps aux | grep [q]bittorrent-nox")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("exec command %s: %w", cmd.String(), err)
	}

	// parse output(likes --webui-password=xxx) to get password
	re := regexp.MustCompile(`--webui-password=(\S+)`)
	matches := re.FindStringSubmatch(string(output))
	if len(matches) > 1 {
		return matches[1], nil
	}

	return "", fmt.Errorf("no qbittorrent-nox process found")
}

// 处理 Unix Socket 连接
func findPassword(ctx *cli.Context) error {
	filePath := ctx.String("file")
	var password = ""
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	// 创建一个 channel 来接收 os 信号
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
	log.Printf("Starting qb password finder...")
	for {
		select {
		case <-ticker.C:
			newPwd, err := fetchQbPassword()
			if err != nil {
				log.Printf("fetch qb password error: %v", err)
				continue
			}
			if newPwd != password {
				password = newPwd
				err := os.WriteFile(filePath, []byte(password), 0666)
				if err != nil {
					log.Printf("write file error: %v", err)
				}
			}
		case <-signalChan:
			log.Println("Received shutdown signal, exiting...")
			return nil
		}
	}
}

func main() {
	app := &cli.App{
		Name:   "fnos-qb-pwd",
		Usage:  "fnos-qb-pwd is a find pwd for qBittorrent in fnOS",
		Action: findPassword,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "file",
				Aliases: []string{"f"},
				Usage:   "pwd file path",
				Value:   pwFile,
			},
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}
