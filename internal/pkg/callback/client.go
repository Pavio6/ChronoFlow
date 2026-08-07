package callback

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/chronoflow/internal/model"
)

type Client struct {
	httpClient      *http.Client
	maxResponseBody int64
	allowPrivate    bool
}

func NewClient(
	timeout time.Duration,
	maxResponseBody int64,
	allowPrivate bool,
) *Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy:               nil,
		ForceAttemptHTTP2:   true,
		MaxIdleConns:        200,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, fmt.Errorf("解析回调地址失败: %w", err)
			}
			addresses, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil {
				return nil, fmt.Errorf("解析回调域名失败: %w", err)
			}
			for _, address := range addresses {
				if !allowPrivate && isPrivateAddress(address) {
					return nil, fmt.Errorf("回调域名解析到禁止的私有地址: %s", address)
				}
			}
			if len(addresses) == 0 {
				return nil, fmt.Errorf("回调域名没有可用地址")
			}
			return dialer.DialContext(
				ctx,
				network,
				net.JoinHostPort(addresses[0].String(), port),
			)
		},
	}
	client := &Client{
		maxResponseBody: maxResponseBody,
		allowPrivate:    allowPrivate,
	}
	client.httpClient = &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("回调重定向次数超过上限")
			}
			return ValidateURL(request.URL.String(), allowPrivate)
		},
	}
	return client
}

func ValidateURL(rawURL string, allowPrivate bool) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("回调 URL 无效: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("回调 URL 只允许 http 或 https")
	}
	if parsed.Hostname() == "" {
		return fmt.Errorf("回调 URL 缺少主机名")
	}
	if parsed.User != nil {
		return fmt.Errorf("回调 URL 不允许包含用户凭据")
	}
	if !allowPrivate {
		if address := net.ParseIP(parsed.Hostname()); address != nil && isPrivateAddress(address) {
			return fmt.Errorf("回调 URL 不允许访问私有或本机地址")
		}
		if strings.EqualFold(parsed.Hostname(), "localhost") {
			return fmt.Errorf("回调 URL 不允许访问 localhost")
		}
	}
	return nil
}

func (c *Client) Execute(
	ctx context.Context,
	snapshot *model.CallbackSnapshot,
	executionID int64,
) (int, string, error) {
	if err := ValidateURL(snapshot.URL, c.allowPrivate); err != nil {
		return 0, "", err
	}
	var body io.Reader
	if snapshot.Body != "" {
		body = bytes.NewBufferString(snapshot.Body)
	}
	request, err := http.NewRequestWithContext(ctx, snapshot.Method, snapshot.URL, body)
	if err != nil {
		return 0, "", fmt.Errorf("创建回调请求失败: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "ChronoFlow/1.0")
	for key, value := range snapshot.Headers {
		if isReservedHeader(key) {
			continue
		}
		request.Header.Set(key, value)
	}
	stableID := strconv.FormatInt(executionID, 10)
	request.Header.Set("Idempotency-Key", "chronoflow-execution-"+stableID)
	request.Header.Set("X-ChronoFlow-Execution-ID", stableID)

	response, err := c.httpClient.Do(request)
	if err != nil {
		return 0, "", fmt.Errorf("执行 HTTP 回调失败: %w", err)
	}
	defer response.Body.Close()

	limited := io.LimitReader(response.Body, c.maxResponseBody+1)
	bodyBytes, err := io.ReadAll(limited)
	if err != nil {
		return response.StatusCode, "", fmt.Errorf("读取回调响应失败: %w", err)
	}
	if int64(len(bodyBytes)) > c.maxResponseBody {
		bodyBytes = bodyBytes[:c.maxResponseBody]
		return response.StatusCode, string(bodyBytes), fmt.Errorf("回调响应体超过 %d 字节上限", c.maxResponseBody)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return response.StatusCode, string(bodyBytes), fmt.Errorf(
			"HTTP 回调返回非 2xx 状态码: %d",
			response.StatusCode,
		)
	}
	return response.StatusCode, string(bodyBytes), nil
}

func isReservedHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "host", "content-length", "connection", "transfer-encoding",
		"idempotency-key", "x-chronoflow-execution-id":
		return true
	default:
		return false
	}
}

func isPrivateAddress(address net.IP) bool {
	return address.IsLoopback() ||
		address.IsPrivate() ||
		address.IsLinkLocalUnicast() ||
		address.IsLinkLocalMulticast() ||
		address.IsUnspecified() ||
		address.IsMulticast()
}
