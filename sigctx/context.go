package sigctx

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/sirupsen/logrus"
)

// SignalContext creates a context that is cancelled when a termination signal is received.
// Note: SIGKILL cannot be caught and is intentionally excluded.
// SIGHUP is excluded by default; if you need config reload, handle it separately.
func SignalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())

	// 设置信号处理 - 只捕获可处理的终止信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	go func() {
		sig := <-sigChan
		logrus.Infof("Received signal [%s], shutting down gracefully...", sig)
		cancel() // 取消上下文
	}()
	return ctx, cancel
}

// NotifyReload returns a channel that receives SIGHUP signals for config reload.
// Callers should range over this channel and trigger config reload logic.
func NotifyReload() <-chan os.Signal {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGHUP)
	return sigChan
}
