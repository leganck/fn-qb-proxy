package sigctx

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/sirupsen/logrus"
)

func SignalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())

	// 设置信号处理
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL, syscall.SIGQUIT, syscall.SIGSTOP, syscall.SIGHUP)

	go func() {
		sig := <-sigChan
		logrus.Infof("Received signal [%s], shutting down gracefully...", sig)
		cancel() // 取消上下文
	}()
	return ctx, cancel
}
