// Package errpage 为反向代理提供后端不可用时的自定义错误页面。
// 内置一个深色主题、支持自动刷新的 HTML 页面；也支持通过文件提供自定义页面。
package errpage

import (
	"fmt"
	"html"
	"net/http"
	"os"
	"strings"

	"github.com/sirupsen/logrus"
)

// 页面模板占位符。
const (
	statusCodeKey = "{{status_code}}"
	statusTextKey = "{{status_text}}"
	messageKey    = "{{message}}"
	errorKey      = "{{error}}"
)

// defaultMessages 面向用户的友好提示，key 为 HTTP 状态码。
var defaultMessages = map[int]string{
	http.StatusBadGateway: "后端服务暂不可用，请稍后重试。",
}

// defaultPage 内置默认错误页面（深色主题，10 秒自动刷新）。
const defaultPage = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="refresh" content="10">
<title>{{status_code}} {{status_text}}</title>
<style>
  :root { color-scheme: dark; }
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body {
    min-height: 100vh;
    display: flex;
    align-items: center;
    justify-content: center;
    background: #10131a;
    color: #e6e8ee;
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto,
                 "PingFang SC", "Microsoft YaHei", sans-serif;
  }
  .card {
    max-width: 480px;
    width: 90%;
    padding: 48px 40px;
    text-align: center;
    background: #1a1f2a;
    border: 1px solid #2a3140;
    border-radius: 16px;
    box-shadow: 0 12px 40px rgba(0, 0, 0, .35);
  }
  .status { font-size: 72px; font-weight: 700; color: #ff8c42; line-height: 1; }
  .status-text { margin-top: 8px; font-size: 20px; color: #c9cedd; }
  .message { margin-top: 16px; font-size: 15px; color: #9aa2b5; line-height: 1.6; }
  .detail {
    margin-top: 20px;
    padding: 12px 14px;
    font-size: 12px;
    color: #7d8597;
    background: #141824;
    border-radius: 8px;
    word-break: break-all;
    text-align: left;
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  }
  .actions { margin-top: 28px; }
  .btn {
    display: inline-block;
    padding: 10px 28px;
    border-radius: 8px;
    background: #ff8c42;
    color: #1a1f2a;
    font-size: 14px;
    font-weight: 600;
    text-decoration: none;
    transition: background .2s;
  }
  .btn:hover { background: #ffa366; }
  .hint { margin-top: 16px; font-size: 12px; color: #5f6878; }
</style>
</head>
<body>
  <div class="card">
    <div class="status">{{status_code}}</div>
    <div class="status-text">{{status_text}}</div>
    <div class="message">{{message}}</div>
    <div class="detail">{{error}}</div>
    <div class="actions"><a class="btn" href="javascript:location.reload()">刷新重试</a></div>
    <div class="hint">页面将在 10 秒后自动刷新</div>
  </div>
</body>
</html>
`

// Handler 返回适合赋值给 httputil.ReverseProxy.ErrorHandler 的错误处理器。
//
// pageFile 为自定义 HTML 错误页路径，为空时使用内置页面。自定义页面同样支持
// 占位符替换：{{status_code}}、{{status_text}}、{{message}}、{{error}}。
// 页面文件在首次调用时加载，读取失败会回退到内置页面并记录警告。
//
// 对非浏览器请求（Accept 不包含 text/html）返回纯文本，保证 API 客户端兼容。
func Handler(pageFile string) func(w http.ResponseWriter, r *http.Request, err error) {
	page := loadPage(pageFile)
	return func(w http.ResponseWriter, r *http.Request, err error) {
		status := http.StatusBadGateway
		msg := defaultMessages[status]
		if msg == "" {
			msg = http.StatusText(status)
		}
		errText := ""
		if err != nil {
			errText = err.Error()
		}

		if !strings.Contains(r.Header.Get("Accept"), "text/html") {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(status)
			fmt.Fprintf(w, "%d %s: %s\n", status, http.StatusText(status), errText)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		body := strings.NewReplacer(
			statusCodeKey, fmt.Sprintf("%d", status),
			statusTextKey, http.StatusText(status),
			messageKey, html.EscapeString(msg),
			errorKey, html.EscapeString(errText),
		).Replace(page)
		_, _ = w.Write([]byte(body))
	}
}

// loadPage 读取自定义错误页；文件不存在或读取失败时回退到内置页面。
func loadPage(pageFile string) string {
	if pageFile == "" {
		return defaultPage
	}
	data, err := os.ReadFile(pageFile)
	if err != nil {
		logrus.Warnf("Failed to read error page %s: %v, using built-in page", pageFile, err)
		return defaultPage
	}
	return string(data)
}
