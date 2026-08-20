package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const maxHTTPResponseBytes int64 = 16 << 20

type transportError struct {
	op     string
	kind   string
	status int
	code   int
	cause  error
}

func (err *transportError) Error() string {
	switch {
	case err.status != 0:
		return fmt.Sprintf("mcp %s: HTTP status %d", err.op, err.status)
	case err.code != 0:
		return fmt.Sprintf("mcp %s: remote JSON-RPC error code %d", err.op, err.code)
	case err.cause != nil:
		return fmt.Sprintf("mcp %s: %s: %v", err.op, err.kind, err.cause)
	default:
		return fmt.Sprintf("mcp %s: %s", err.op, err.kind)
	}
}

func (err *transportError) Unwrap() error { return err.cause }

type HTTPTransportOptions struct {
	Endpoint   string
	Headers    http.Header
	Timeout    time.Duration
	HTTPClient *http.Client
}

type HTTPTransport struct {
	endpoint *url.URL
	headers  http.Header
	timeout  time.Duration
	client   *http.Client

	sessionMu sync.RWMutex
	sessionID string
}

func NewHTTPTransport(options HTTPTransportOptions) (*HTTPTransport, error) {
	endpoint, err := options.validateEndpoint()
	if err != nil {
		return nil, err
	}
	if options.Timeout <= 0 {
		return nil, errors.New("mcp HTTP timeout must be greater than zero")
	}
	headers, err := options.validateHeaders()
	if err != nil {
		return nil, err
	}
	client := &http.Client{}
	if options.HTTPClient != nil {
		*client = *options.HTTPClient
	}
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) == 0 || !strings.EqualFold(request.URL.Host, via[0].URL.Host) {
			return errors.New("mcp redirect to a different host is not allowed")
		}
		return nil
	}
	return &HTTPTransport{endpoint: endpoint, headers: headers, timeout: options.Timeout, client: client}, nil
}

// validateEndpoint 校验并解析传输端点：必须是绝对的 HTTP(S) URL，
// 明文 HTTP 只允许 loopback。
func (options HTTPTransportOptions) validateEndpoint() (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(options.Endpoint))
	if err != nil || parsed.Host == "" {
		return nil, errors.New("mcp endpoint must be an absolute URL")
	}
	if parsed.User != nil {
		return nil, errors.New("mcp endpoint must not contain user information")
	}
	if parsed.RawQuery != "" {
		return nil, errors.New("mcp endpoint must not contain a query")
	}
	if parsed.Fragment != "" {
		return nil, errors.New("mcp endpoint must not contain a fragment")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https":
	case "http":
		host := parsed.Hostname()
		ip := net.ParseIP(host)
		if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
			return nil, errors.New("mcp plaintext HTTP endpoint must be loopback")
		}
	default:
		return nil, errors.New("mcp endpoint scheme must be HTTP or HTTPS")
	}
	return parsed, nil
}

// validateHeaders 规范化并校验自定义 Header：名称必须合法，不能覆盖
// 协议控制 Header，值必须合法。
func (options HTTPTransportOptions) validateHeaders() (http.Header, error) {
	input := options.Headers
	result := make(http.Header, len(input))
	blocked := map[string]struct{}{
		"Host": {}, "Content-Length": {}, "Mcp-Session-Id": {}, "Content-Type": {}, "Accept": {},
	}
	for key, values := range input {
		trimmed := strings.TrimSpace(key)
		if !validHTTPHeaderName(trimmed) {
			return nil, errors.New("mcp header name is invalid")
		}
		canonical := http.CanonicalHeaderKey(trimmed)
		if _, denied := blocked[canonical]; denied {
			return nil, fmt.Errorf("mcp header %q is controlled by the transport", canonical)
		}
		for _, value := range values {
			if !validHTTPHeaderValue(value) {
				return nil, fmt.Errorf("mcp header %q contains an invalid value", canonical)
			}
		}
		result[canonical] = append([]string(nil), values...)
	}
	return result, nil
}

func validHTTPHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for index := 0; index < len(name); index++ {
		character := name[index]
		if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || strings.ContainsRune("!#$%&'*+-.^_`|~", rune(character))) {
			return false
		}
	}
	return true
}

func validHTTPHeaderValue(value string) bool {
	for index := 0; index < len(value); index++ {
		character := value[index]
		if (character < 0x20 && character != '\t') || character == 0x7f {
			return false
		}
	}
	return true
}

func (transport *HTTPTransport) Send(ctx context.Context, request Request) (Response, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return Response{}, &transportError{op: request.Method, kind: "encode request", cause: err}
	}
	requestCtx, cancel := context.WithTimeout(ctx, transport.timeout)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(requestCtx, http.MethodPost, transport.endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return Response{}, &transportError{op: request.Method, kind: "create request", cause: err}
	}
	transport.applyHeaders(httpRequest)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json, text/event-stream")

	httpResponse, err := transport.client.Do(httpRequest)
	if err != nil {
		return Response{}, &transportError{op: request.Method, kind: "request failed", cause: err}
	}
	defer httpResponse.Body.Close()
	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		return Response{}, &transportError{op: request.Method, status: httpResponse.StatusCode}
	}
	transport.captureSession(httpResponse.Header.Get("Mcp-Session-Id"))
	if request.ID == nil && (httpResponse.StatusCode == http.StatusAccepted || httpResponse.StatusCode == http.StatusNoContent) {
		return Response{}, nil
	}
	body, err := readLimitedBody(httpResponse.Body)
	if err != nil {
		return Response{}, &transportError{op: request.Method, kind: "read response", cause: err}
	}
	mediaType, _, err := mime.ParseMediaType(httpResponse.Header.Get("Content-Type"))
	if err != nil {
		return Response{}, &transportError{op: request.Method, kind: "invalid response content type", cause: err}
	}
	var responses []Response
	switch mediaType {
	case "application/json":
		response, err := decodeResponse(body)
		if err != nil {
			return Response{}, &transportError{op: request.Method, kind: "decode JSON response", cause: err}
		}
		responses = []Response{response}
	case "text/event-stream":
		responses, err = decodeSSEResponses(body)
		if err != nil {
			return Response{}, &transportError{op: request.Method, kind: "decode SSE response", cause: err}
		}
	default:
		return Response{}, &transportError{op: request.Method, kind: "unsupported response content type"}
	}
	return request.selectResponse(responses)
}

func (transport *HTTPTransport) applyHeaders(request *http.Request) {
	for key, values := range transport.headers {
		request.Header[key] = append([]string(nil), values...)
	}
	transport.sessionMu.RLock()
	sessionID := transport.sessionID
	transport.sessionMu.RUnlock()
	if sessionID != "" {
		request.Header.Set("Mcp-Session-Id", sessionID)
	}
}

func (transport *HTTPTransport) captureSession(value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	transport.sessionMu.Lock()
	transport.sessionID = value
	transport.sessionMu.Unlock()
}

func readLimitedBody(reader io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, maxHTTPResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxHTTPResponseBytes {
		return nil, errors.New("response is too large")
	}
	return body, nil
}

func decodeResponse(data []byte) (Response, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var response Response
	if err := decoder.Decode(&response); err != nil {
		return Response{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return Response{}, err
		}
		return Response{}, errors.New("response contains trailing JSON")
	}
	return response, nil
}

func decodeSSEResponses(data []byte) ([]Response, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), int(maxHTTPResponseBytes))
	var responses []Response
	var dataLines []string
	flush := func() error {
		if len(dataLines) == 0 {
			return nil
		}
		response, err := decodeResponse([]byte(strings.Join(dataLines, "\n")))
		if err != nil {
			return err
		}
		responses = append(responses, response)
		dataLines = nil
		return nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				return nil, err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "data:") {
			value := strings.TrimPrefix(line, "data:")
			value = strings.TrimPrefix(value, " ")
			dataLines = append(dataLines, value)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if err := flush(); err != nil {
		return nil, err
	}
	if len(responses) == 0 {
		return nil, errors.New("SSE response contains no JSON-RPC message")
	}
	return responses, nil
}

// selectResponse 在候选响应中选择与本请求匹配的 JSON-RPC 响应。
func (request Request) selectResponse(responses []Response) (Response, error) {
	for _, response := range responses {
		if response.JSONRPC != "2.0" {
			continue
		}
		if request.ID == nil || (response.ID != nil && *response.ID == *request.ID) {
			if response.Error != nil {
				return Response{}, &transportError{op: request.Method, code: response.Error.Code}
			}
			return response, nil
		}
	}
	return Response{}, &transportError{op: request.Method, kind: "matching JSON-RPC response not found"}
}

func (transport *HTTPTransport) Close(ctx context.Context) error {
	defer transport.closeIdleConnections()
	transport.sessionMu.RLock()
	sessionID := transport.sessionID
	transport.sessionMu.RUnlock()
	if sessionID == "" {
		return nil
	}
	requestCtx, cancel := context.WithTimeout(ctx, transport.timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodDelete, transport.endpoint.String(), nil)
	if err != nil {
		return &transportError{op: "close session", kind: "create request", cause: err}
	}
	transport.applyHeaders(request)
	response, err := transport.client.Do(request)
	if err != nil {
		return &transportError{op: "close session", kind: "request failed", cause: err}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &transportError{op: "close session", status: response.StatusCode}
	}
	transport.sessionMu.Lock()
	transport.sessionID = ""
	transport.sessionMu.Unlock()
	return nil
}

func (transport *HTTPTransport) closeIdleConnections() {
	transport.client.CloseIdleConnections()
}
