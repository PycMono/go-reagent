package middleware

import (
	"fmt"
	"net"
	"net/netip"
	"strings"

	"github.com/gin-gonic/gin"
)

// W3C Trace Context Header（§7、§16.3：只认 W3C，不解析 B3）。
const (
	headerTraceParent = "traceparent"
	headerTraceState  = "tracestate"
)

// TraceContextBoundary 是公网入口的信任边界（§7）：只有配置的可信上游
// （按 RemoteIP 匹配）可以保留 Remote Parent；其他请求先删除
// traceparent/tracestate，再由 Tracing 中间件创建内部 root Span。
// 必须在 middleware.Tracing() 之前安装。
//
// trustedUpstreams 为 IP 或 CIDR 列表；空列表表示不信任任何上游。
// TraceID 仅用于技术关联，不得用于鉴权、幂等或业务身份。
func TraceContextBoundary(trustedUpstreams []string) (gin.HandlerFunc, error) {
	trusted, err := parseTrustedUpstreams(trustedUpstreams)
	if err != nil {
		return nil, err
	}
	return func(c *gin.Context) {
		if c.Request.Header.Get(headerTraceParent) == "" && c.Request.Header.Get(headerTraceState) == "" {
			c.Next()
			return
		}
		if isTrustedUpstream(c.Request.RemoteAddr, trusted) {
			c.Next()
			return
		}
		c.Request.Header.Del(headerTraceParent)
		c.Request.Header.Del(headerTraceState)
		c.Next()
	}, nil
}

type trustedUpstreams struct {
	addresses map[netip.Addr]struct{}
	prefixes  []netip.Prefix
}

func parseTrustedUpstreams(entries []string) (trustedUpstreams, error) {
	trusted := trustedUpstreams{addresses: make(map[netip.Addr]struct{})}
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			return trustedUpstreams{}, fmt.Errorf("trace context boundary: empty trusted upstream")
		}
		if strings.Contains(entry, "/") {
			prefix, err := netip.ParsePrefix(entry)
			if err != nil {
				return trustedUpstreams{}, fmt.Errorf("trace context boundary: invalid CIDR %q", entry)
			}
			trusted.prefixes = append(trusted.prefixes, prefix.Masked())
			continue
		}
		address, err := netip.ParseAddr(entry)
		if err != nil {
			return trustedUpstreams{}, fmt.Errorf("trace context boundary: invalid IP %q", entry)
		}
		trusted.addresses[address] = struct{}{}
	}
	return trusted, nil
}

func (trusted trustedUpstreams) contains(address netip.Addr) bool {
	if _, ok := trusted.addresses[address]; ok {
		return true
	}
	for _, prefix := range trusted.prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

// isTrustedUpstream 从 RemoteAddr（host:port）解析来源 IP 并匹配可信集合；
// 解析失败一律按不可信处理。
func isTrustedUpstream(remoteAddr string, trusted trustedUpstreams) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	address, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil {
		return false
	}
	return trusted.contains(address)
}
