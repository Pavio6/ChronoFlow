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

// NewClient 创建带回调安全校验和响应体限制的 HTTP 客户端。
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
				return nil, fmt.Errorf("parse callback address: %w", err)
			}
			addresses, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil {
				return nil, fmt.Errorf("resolve callback hostname: %w", err)
			}
			for _, address := range addresses {
				if !allowPrivate && isPrivateAddress(address) {
					return nil, fmt.Errorf("callback hostname resolves to a disallowed private address: %s", address)
				}
			}
			if len(addresses) == 0 {
				return nil, fmt.Errorf("callback hostname has no usable addresses")
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
				return fmt.Errorf("callback redirect limit exceeded")
			}
			return ValidateURL(request.URL.String(), allowPrivate)
		},
	}
	return client
}

// ValidateURL 校验回调地址的协议、主机和私网访问限制。
func ValidateURL(rawURL string, allowPrivate bool) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid callback URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("callback URL scheme must be http or https")
	}
	if parsed.Hostname() == "" {
		return fmt.Errorf("callback URL host is required")
	}
	if parsed.User != nil {
		return fmt.Errorf("callback URL must not include user credentials")
	}
	if !allowPrivate {
		if address := net.ParseIP(parsed.Hostname()); address != nil && isPrivateAddress(address) {
			return fmt.Errorf("callback URL must not target a private or loopback address")
		}
		if strings.EqualFold(parsed.Hostname(), "localhost") {
			return fmt.Errorf("callback URL must not target localhost")
		}
	}
	return nil
}

// Execute 使用固定的请求快照执行回调，并返回状态码和响应体。
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
		return 0, "", fmt.Errorf("create callback request: %w", err)
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
		return 0, "", fmt.Errorf("execute HTTP callback: %w", err)
	}
	defer response.Body.Close()

	limited := io.LimitReader(response.Body, c.maxResponseBody+1)
	bodyBytes, err := io.ReadAll(limited)
	if err != nil {
		return response.StatusCode, "", fmt.Errorf("read callback response: %w", err)
	}
	if int64(len(bodyBytes)) > c.maxResponseBody {
		bodyBytes = bodyBytes[:c.maxResponseBody]
		return response.StatusCode, string(bodyBytes), fmt.Errorf("callback response exceeds maximum size of %d bytes", c.maxResponseBody)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return response.StatusCode, string(bodyBytes), fmt.Errorf(
			"HTTP callback returned a non-2xx status code: %d",
			response.StatusCode,
		)
	}
	return response.StatusCode, string(bodyBytes), nil
}

// isReservedHeader 判断请求头是否由系统保留且不允许回调配置覆盖。
func isReservedHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "host", "content-length", "connection", "transfer-encoding",
		"idempotency-key", "x-chronoflow-execution-id":
		return true
	default:
		return false
	}
}

// isPrivateAddress 判断 IP 是否属于不应默认访问的私有或特殊地址。
func isPrivateAddress(address net.IP) bool {
	// 回环地址，例如 127.0.0.1、::1，只指向当前机器。
	return address.IsLoopback() ||
		// 私有网络地址，例如 10.0.0.0/8、172.16.0.0/12、192.168.0.0/16。
		address.IsPrivate() ||
		// 单播链路本地地址，例如 IPv4 的 169.254.0.0/16，只在当前二层网络有效。
		address.IsLinkLocalUnicast() ||
		// 组播链路本地地址，只能在当前网络链路内传播。
		address.IsLinkLocalMulticast() ||
		// 未指定地址，例如 0.0.0.0、::，不能作为实际远程目标。
		address.IsUnspecified() ||
		// 组播地址，用于向一组主机发送数据，不是单个 HTTP 回调目标。
		address.IsMulticast()
}
