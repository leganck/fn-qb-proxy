package errpage

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestHandlerHTML(t *testing.T) {
	h := Handler("")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "text/html")

	h(rec, req, &customErr{"dial unix <sock> failed: connect & reset"})

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("content-type = %q, want text/html", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{"502", "Bad Gateway", "后端服务暂不可用", "刷新重试"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
	if strings.Contains(body, "dial unix <sock>") || strings.Contains(body, "& reset") {
		t.Errorf("error detail should be HTML-escaped, got raw: %s", body)
	}
	if !strings.Contains(body, "dial unix &lt;sock&gt; failed: connect &amp; reset") {
		t.Errorf("body missing escaped error detail: %s", body)
	}
}

func TestHandlerPlain(t *testing.T) {
	h := Handler("")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v2/torrents/info", nil)
	req.Header.Set("Accept", "application/json")

	h(rec, req, &customErr{"boom"})

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/plain") {
		t.Fatalf("content-type = %q, want text/plain", ct)
	}
	if body := rec.Body.String(); !strings.Contains(body, "boom") {
		t.Errorf("body = %q, want to contain error detail", body)
	}
}

func TestHandlerCustomPage(t *testing.T) {
	dir := t.TempDir()
	pagePath := dir + "/err.html"
	if err := os.WriteFile(pagePath, []byte("<h1>{{status_code}} - {{message}}</h1><p>{{error}}</p>"), 0644); err != nil {
		t.Fatal(err)
	}

	h := Handler(pagePath)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "text/html")

	h(rec, req, &customErr{"custom detail"})

	body := rec.Body.String()
	for _, want := range []string{"<h1>502 - 后端服务暂不可用，请稍后重试。</h1>", "<p>custom detail</p>"} {
		if !strings.Contains(body, want) {
			t.Errorf("custom page body missing %q, got: %s", want, body)
		}
	}
}

func TestHandlerCustomPageMissing(t *testing.T) {
	h := Handler("/nonexistent/page.html")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "text/html")

	h(rec, req, &customErr{"x"})

	if !strings.Contains(rec.Body.String(), "刷新重试") {
		t.Errorf("missing page should fall back to built-in, got: %s", rec.Body.String())
	}
}

type customErr struct{ msg string }

func (e *customErr) Error() string { return e.msg }
