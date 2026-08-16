# 通用聊天 Skills 与工具实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为单浏览器 Chat Agent 增加真实天气、当地时间、安全数值计算和四个按需加载的通用聊天 Skill，同时保持 Web 只读、Direct Loop 与现有持久化/API 契约不变。

**Architecture:** 天气接口定义在 `domain/service`，Open-Meteo 协议封装在 `infrastructure/driver/openmeteo`，三个业务 Tool 在 `application/tool/chat` 实现并通过 Fx 分组显式装配到 Web。四个 Skill 只存在于 `workspaces/chat/skills`，由现有 `pi/harness/skills` 每次 Run 发现；不修改 `pi` 核心、数据库或前端。

**Tech Stack:** Go 1.26、Uber Fx、`net/http`、`httptest.Server`、`github.com/expr-lang/expr`、现有 `pi/ai.Tool` 与 Workspace Skill Runtime

**Spec:** `docs/superpowers/specs/2026-08-16-general-chat-skills-tools-design.md`

## Global Constraints

- 第一期恰好增加 4 个 Skill：`weather-assistance`、`writing-assistance`、`decision-support`、`learning-explanation`。
- 第一期恰好增加 3 个 Tool：`get_weather`、`get_current_time`、`calculate`。
- Web 最终 Tool 集合按名称排序必须是 `calculate`、`get_current_time`、`get_weather`、`read`。
- 三个新 Tool 的 Definition 都设置 `ParallelSafe:true`，并沿用 Scheduler 单轮最多 4 个并发调用。
- Web 不得注册 `apply_patch`、`edit`、`exec`、`process` 或 `write`，并继续使用 `pi.ThinkingEnabled(false)`。
- Open-Meteo 生产端点固定为 HTTPS，请求总超时 10 秒，响应上限 1 MiB，不重试、不缓存、不泄露原始响应正文。
- 用户不能传 URL、Host、经纬度、Header 或 HTTP 方法；测试不得访问真实 Open-Meteo。
- 天气返回区间完整落在地点当地 `[today, today+6]`，地点歧义不得默认选择第一个候选。
- 计算表达式只允许纯数值算术 AST，不注入变量、对象、函数、文件、网络或进程能力。
- Skill 只创建 `SKILL.md`，Frontmatter 只有 `name`、`description`；不创建 `agents/openai.yaml`、README、脚本或参考文件。
- Skill 对照评测只读工作区，不访问 Open-Meteo、模型外部 API 或数据库，原始结果只写 `/tmp`。
- 不修改数据库 Migration、HTTP API、Cookie 身份、Conversation、SSE、页面布局、`pi.Register` 或 `pi.CodingToolsRegister`。
- 保留用户在 `pi/recovery.go` 和 `pi/test/recovery_test.go` 中的未提交修改，不暂存、不覆盖。

---

### Task 1: 天气领域契约与 Open-Meteo 地理编码

**Files:**
- Create: `domain/service/weather.go`
- Create: `infrastructure/driver/openmeteo/client.go`
- Create: `infrastructure/driver/openmeteo/client_test.go`

**Interfaces:**
- Produces: `service.LocationQuery`、`service.Location`、`service.ForecastRequest`、`service.DailyForecast`、`service.Forecast`
- Produces: `service.LocationResolver.ResolveLocations(context.Context, service.LocationQuery) ([]service.Location, error)`
- Produces: `service.WeatherProvider.Forecast(context.Context, service.ForecastRequest) (service.Forecast, error)`
- Produces: `openmeteo.NewClient() *Client` 和包内 `newClient(geocodingURL, forecastURL string, httpClient *http.Client) *Client`

- [ ] **Step 1: 写地理编码 HTTP 边界测试**

在 `client_test.go` 使用 `httptest.Server` 写真实 HTTP 契约测试，断言查询参数、映射、空结果、非 2xx、重定向拒绝、畸形 JSON、超大响应、Context 取消和超时。成功 fixture 使用完整上游结构：

```go
const geocodingFixture = `{
  "results": [{
    "name": "Beijing", "country": "China", "country_code": "CN",
    "admin1": "Beijing", "latitude": 39.9042, "longitude": 116.4074,
    "timezone": "Asia/Shanghai"
  }]
}`

func TestResolveLocationsMapsCandidatesAndEncodesQuery(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path != "/v1/search" || r.URL.Query().Get("name") != "北京" ||
            r.URL.Query().Get("count") != "5" || r.Header.Get("User-Agent") == "" {
            t.Fatalf("request = %s %#v", r.URL.String(), r.Header)
        }
        w.Header().Set("Content-Type", "application/json")
        io.WriteString(w, geocodingFixture)
    }))
    defer server.Close()

    client := newClient(server.URL+"/v1/search", server.URL+"/v1/forecast", server.Client())
    got, err := client.ResolveLocations(context.Background(), service.LocationQuery{Name: "北京", Limit: 5})
    if err != nil { t.Fatal(err) }
    want := service.Location{Name: "Beijing", Country: "China", CountryCode: "CN", Admin1: "Beijing", Latitude: 39.9042, Longitude: 116.4074, Timezone: "Asia/Shanghai"}
    if len(got) != 1 || got[0] != want { t.Fatalf("locations = %#v", got) }
}
```

错误测试只断言稳定分类和脱敏边界：错误中不得出现 fixture 正文、用户完整查询 URL 或测试 secret。

- [ ] **Step 2: 运行测试确认 RED**

Run: `go test ./infrastructure/driver/openmeteo -run 'TestResolveLocations'`

Expected: FAIL，原因是天气领域接口与 Open-Meteo Client 尚不存在。

- [ ] **Step 3: 定义领域契约**

```go
package service

import (
    "context"
    "time"
)

type LocationQuery struct { Name, CountryCode, Admin1 string; Limit int }
type Location struct {
    Name, Country, CountryCode, Admin1 string
    Latitude, Longitude float64
    Timezone string
}
type ForecastRequest struct { Location Location; StartDate time.Time; Days int }
type DailyForecast struct {
    Date, Condition string
    WeatherCode, PrecipitationProbability int
    TemperatureMinC, TemperatureMaxC, WindSpeedMaxKPH float64
}
type Forecast struct { Location Location; Days []DailyForecast }

type LocationResolver interface {
    ResolveLocations(context.Context, LocationQuery) ([]Location, error)
}
type WeatherProvider interface {
    Forecast(context.Context, ForecastRequest) (Forecast, error)
}
```

`Forecast` 不携带 `GeneratedAt`：该字段是 Application Tool 的执行时间，不是 Open-Meteo 领域数据；Task 3 使用注入 Clock 生成它，避免 Infrastructure Driver 持有业务时钟。

- [ ] **Step 4: 实现固定生产端点和有界 JSON 解码**

```go
const (
    defaultGeocodingURL = "https://geocoding-api.open-meteo.com/v1/search"
    defaultForecastURL  = "https://api.open-meteo.com/v1/forecast"
    maxResponseBytes    = 1 << 20
)

func NewClient() *Client {
    return newClient(defaultGeocodingURL, defaultForecastURL, &http.Client{
        Timeout: 10 * time.Second,
        CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
    })
}

func decodeResponse(response *http.Response, target any) error {
    if response.StatusCode < 200 || response.StatusCode >= 300 {
        return fmt.Errorf("Open-Meteo returned HTTP %d", response.StatusCode)
    }
    data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
    if err != nil { return fmt.Errorf("read Open-Meteo response: %w", err) }
    if len(data) > maxResponseBytes { return errors.New("Open-Meteo response exceeds 1 MiB") }
    decoder := json.NewDecoder(bytes.NewReader(data))
    if err := decoder.Decode(target); err != nil { return errors.New("Open-Meteo returned invalid JSON") }
    var extra any
    if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) { return errors.New("Open-Meteo returned trailing JSON") }
    return nil
}
```

`ResolveLocations` 只能基于构造函数保存的端点构造 GET 请求，固定 `count<=5`、设置 `User-Agent`，并把每个必需字段缺失视为非法上游响应。不要使用 `DisallowUnknownFields`，以允许上游新增无关字段。

- [ ] **Step 5: 运行地理编码聚焦测试**

Run: `go test ./domain/... ./infrastructure/driver/openmeteo -run 'TestResolveLocations'`

Expected: PASS，测试只访问 `httptest.Server`。

- [ ] **Step 6: 提交**

```bash
git add domain/service/weather.go infrastructure/driver/openmeteo/client.go infrastructure/driver/openmeteo/client_test.go
git commit -m "feat: add Open-Meteo location resolver"
```

### Task 2: Open-Meteo 每日天气预报

**Files:**
- Modify: `infrastructure/driver/openmeteo/client.go`
- Modify: `infrastructure/driver/openmeteo/client_test.go`

**Interfaces:**
- Consumes: Task 1 的 `service.WeatherProvider` 契约和 `Client`
- Produces: `(*Client).Forecast(context.Context, service.ForecastRequest) (service.Forecast, error)`

- [ ] **Step 1: 写预报请求与响应校验测试**

成功测试断言 `latitude`、`longitude`、`timezone`、`start_date`、`end_date` 和五个 daily 字段准确传递；表驱动错误测试覆盖日期错位、数组长度不一致、缺失 daily、非 2xx、畸形 JSON、超大响应、取消和超时。

```go
const forecastFixture = `{
  "daily": {
    "time": ["2026-08-16", "2026-08-17"],
    "weather_code": [0, 61],
    "temperature_2m_min": [22.5, 23.1],
    "temperature_2m_max": [30.0, 31.2],
    "precipitation_probability_max": [10, 70],
    "wind_speed_10m_max": [8.2, 18.4]
  }
}`

func TestForecastMapsRequestedDailyRange(t *testing.T) {
    request := service.ForecastRequest{
        Location: service.Location{Name: "Beijing", Latitude: 39.9042, Longitude: 116.4074, Timezone: "Asia/Shanghai"},
        StartDate: time.Date(2026, 8, 16, 0, 0, 0, 0, time.FixedZone("CST", 8*60*60)), Days: 2,
    }
    got, err := clientForForecastFixture(t, forecastFixture).Forecast(context.Background(), request)
    if err != nil { t.Fatal(err) }
    if len(got.Days) != 2 || got.Days[1].Date != "2026-08-17" || got.Days[1].Condition != "rain" {
        t.Fatalf("forecast = %#v", got)
    }
}

func clientForForecastFixture(t *testing.T, fixture string) *Client {
    t.Helper()
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Query().Get("start_date") != "2026-08-16" || r.URL.Query().Get("end_date") != "2026-08-17" ||
            r.URL.Query().Get("timezone") != "Asia/Shanghai" {
            t.Fatalf("forecast query = %q", r.URL.RawQuery)
        }
        w.Header().Set("Content-Type", "application/json")
        io.WriteString(w, fixture)
    }))
    t.Cleanup(server.Close)
    return newClient(server.URL+"/v1/search", server.URL+"/v1/forecast", server.Client())
}
```

- [ ] **Step 2: 运行测试确认 RED**

Run: `go test ./infrastructure/driver/openmeteo -run 'TestForecast'`

Expected: FAIL，`Forecast` 尚未实现。

- [ ] **Step 3: 实现每日字段映射与日期序列校验**

```go
func conditionForCode(code int) string {
    switch {
    case code == 0: return "clear"
    case code >= 1 && code <= 3: return "partly_cloudy"
    case code == 45 || code == 48: return "fog"
    case code >= 51 && code <= 57: return "drizzle"
    case (code >= 61 && code <= 67) || (code >= 80 && code <= 82): return "rain"
    case (code >= 71 && code <= 77) || code == 85 || code == 86: return "snow"
    case code == 95 || code == 96 || code == 99: return "thunderstorm"
    default: return "unknown"
    }
}
```

在映射前生成从 `StartDate` 到 `StartDate.AddDate(0, 0, Days-1)` 的连续字面日期切片，与 `daily.time` 逐项比较；六个数组长度都必须等于 `Days`。未知 WMO code 保留整数并返回 `condition="unknown"`。

- [ ] **Step 4: 运行 Driver 全部测试**

Run: `go test ./infrastructure/driver/openmeteo`

Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add infrastructure/driver/openmeteo/client.go infrastructure/driver/openmeteo/client_test.go
git commit -m "feat: add Open-Meteo daily forecast"
```

### Task 3: 地点解析与 `get_weather` Tool

**Files:**
- Create: `application/tool/chat/location.go`
- Create: `application/tool/chat/output.go`
- Create: `application/tool/chat/weather.go`
- Create: `application/tool/chat/weather_test.go`

**Interfaces:**
- Consumes: `service.LocationResolver`、`service.WeatherProvider`、`pi/ai.Tool`
- Produces: 包内 `type Clock func() time.Time`、`newWeatherTool(service.LocationResolver, service.WeatherProvider, Clock) *weatherTool`
- Produces: Tool Definition `get_weather`，正常状态 `ok|not_found|ambiguous`

- [ ] **Step 1: 写 Tool Schema、消歧、日期和输出测试**

使用内存 fake Resolver/Provider，直接执行真实 Tool。测试必须以字面 JSON 解码 Tool 文本，不断言 fake 自身：

```go
func TestWeatherToolReturnsAmbiguousCandidatesWithoutForecast(t *testing.T) {
    resolver := &fakeResolver{locations: []service.Location{
        {Name: "Chaoyang", Country: "China", CountryCode: "CN", Admin1: "Beijing", Timezone: "Asia/Shanghai"},
        {Name: "Chaoyang", Country: "China", CountryCode: "CN", Admin1: "Liaoning", Timezone: "Asia/Shanghai"},
    }}
    provider := &fakeWeatherProvider{}
    tool := newWeatherTool(resolver, provider, fixedClock("2026-08-16T01:30:00Z"))
    output, err := tool.Execute(context.Background(), json.RawMessage(`{"location":"朝阳"}`), nil)
    if err != nil { t.Fatal(err) }
    got := decodeToolJSON[locationFailure](t, output)
    if got.Status != "ambiguous" || len(got.Candidates) != 2 || provider.calls != 0 {
        t.Fatalf("result = %#v, forecast calls = %d", got, provider.calls)
    }
}
```

表驱动覆盖：唯一地点、空白地点、无地点、国家/行政区忽略大小写完整消歧、today、tomorrow、显式日期、时区跨日、今天起 7 天、明天起 6 天、结束日越界、无效时区、上游错误和 Schema 中 `additionalProperties=false`。

- [ ] **Step 2: 运行测试确认 RED**

Run: `go test ./application/tool/chat -run 'TestWeatherTool'`

Expected: FAIL，包和 Tool 尚不存在。

- [ ] **Step 3: 实现共享地点状态和严格 JSON 输出**

```go
type Clock func() time.Time
type locationInput struct { Location, CountryCode, Admin1 string }
type locationView struct {
    Name string `json:"name"`
    Country string `json:"country"`
    CountryCode string `json:"country_code"`
    Admin1 string `json:"admin1"`
    Timezone string `json:"timezone"`
}
type locationFailure struct {
    Status string `json:"status"`
    Query string `json:"query"`
    Candidates []locationView `json:"candidates,omitempty"`
}
type weatherDayView struct {
    Date string `json:"date"`
    WeatherCode int `json:"weather_code"`
    Condition string `json:"condition"`
    TemperatureMinC float64 `json:"temperature_min_c"`
    TemperatureMaxC float64 `json:"temperature_max_c"`
    PrecipitationProbability int `json:"precipitation_probability"`
    WindSpeedMaxKPH float64 `json:"wind_speed_max_kph"`
}
type weatherResult struct {
    Status string `json:"status"`
    Location locationView `json:"location"`
    GeneratedAt string `json:"generated_at"`
    Days []weatherDayView `json:"days"`
}

func jsonOutput(value any) (ai.ToolOutput, error) {
    data, err := json.Marshal(value)
    if err != nil { return ai.ToolOutput{}, fmt.Errorf("encode tool output: %w", err) }
    return ai.ToolOutput{Content: []ai.ContentBlock{ai.TextBlock(string(data))}}, nil
}

func decodeArguments[T any](raw json.RawMessage) (T, error) {
    var value T
    decoder := json.NewDecoder(bytes.NewReader(raw))
    decoder.DisallowUnknownFields()
    if err := decoder.Decode(&value); err != nil {
        return value, invalidArguments("invalid JSON arguments", err)
    }
    var extra any
    if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
        if err == nil { err = errors.New("trailing JSON value") }
        return value, invalidArguments("invalid JSON arguments", err)
    }
    return value, nil
}

func newSystemClock() Clock { return time.Now }

func invalidArguments(message string, cause error) error {
    if cause == nil { cause = errors.New(message) }
    return pierrors.Wrap(pierrors.ErrorCodeToolInvalidArguments, "chat tool arguments", cause)
}
```

所有 Tool 的 `Execute` 先调用 `decodeArguments`，所以直接调用与经 ToolRuntime 调用都拒绝未知字段和尾随 JSON。`resolveLocation` 再 Trim 并检查长度；空地点用 `pierrors.Wrap(pierrors.ErrorCodeToolInvalidArguments, "chat tool arguments", errors.New("location is required"))`，非空 `country_code` 必须是两个 ASCII 字母。查询固定 `Limit=5`，再用 `strings.EqualFold` 对 `country_code`、`admin1` 做完整值过滤。0 个候选返回 `not_found`，多个返回最多 5 个候选且不暴露经纬度。

测试辅助函数在 `weather_test.go` 中定义一次并供本包测试复用：

```go
type fakeResolver struct { locations []service.Location; err error }
func (f *fakeResolver) ResolveLocations(context.Context, service.LocationQuery) ([]service.Location, error) {
    return append([]service.Location(nil), f.locations...), f.err
}

type fakeWeatherProvider struct { forecast service.Forecast; err error; calls int }
func (f *fakeWeatherProvider) Forecast(context.Context, service.ForecastRequest) (service.Forecast, error) {
    f.calls++
    return f.forecast, f.err
}

func fixedClock(value string) Clock {
    instant := time.Date(2026, 8, 16, 1, 30, 0, 0, time.UTC)
    if value != "2026-08-16T01:30:00Z" { panic("unsupported test clock") }
    return func() time.Time { return instant }
}

func decodeToolJSON[T any](t *testing.T, output ai.ToolOutput) T {
    t.Helper()
    text, err := ai.TextContent(output.Content)
    if err != nil { t.Fatal(err) }
    var value T
    if err := json.Unmarshal([]byte(text), &value); err != nil { t.Fatal(err) }
    return value
}
```

- [ ] **Step 4: 实现 `get_weather` 日期窗口与结果**

```go
type weatherArgs struct {
    Location string `json:"location"`
    CountryCode string `json:"country_code,omitempty"`
    Admin1 string `json:"admin1,omitempty"`
    Date string `json:"date,omitempty"`
    Days *int `json:"days,omitempty"`
}

func localDate(clock Clock, timezone string) (time.Time, *time.Location, error) {
    zone, err := time.LoadLocation(timezone)
    if err != nil { return time.Time{}, nil, invalidArguments("invalid location timezone", err) }
    now := clock().In(zone)
    return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, zone), zone, nil
}
```

严格解析 `today|tomorrow|2006-01-02`，并检查 `start>=today` 且 `start+days-1<=today+6`。`generated_at` 使用同一 Clock 转换到地点时区。Tool Definition 设置中文用途说明、设计文档中的完整 Schema 和 `ParallelSafe:true`。

- [ ] **Step 5: 运行聚焦测试**

Run: `go test ./application/tool/chat -run 'TestWeatherTool'`

Expected: PASS。

- [ ] **Step 6: 提交**

```bash
git add application/tool/chat/location.go application/tool/chat/output.go application/tool/chat/weather.go application/tool/chat/weather_test.go
git commit -m "feat: add weather chat tool"
```

### Task 4: `get_current_time` Tool

**Files:**
- Create: `application/tool/chat/current_time.go`
- Create: `application/tool/chat/current_time_test.go`

**Interfaces:**
- Consumes: Task 3 的 `resolveLocation`、`Clock` 和 `jsonOutput`
- Produces: `newCurrentTimeTool(service.LocationResolver, Clock) *currentTimeTool` 和 Tool Definition `get_current_time`

- [ ] **Step 1: 写固定 Clock 的时区行为测试**

```go
func TestCurrentTimeToolUsesResolvedTimezone(t *testing.T) {
    resolver := &fakeResolver{locations: []service.Location{
        {Name: "Tokyo", Country: "Japan", CountryCode: "JP", Admin1: "Tokyo", Timezone: "Asia/Tokyo"},
    }}
    tool := newCurrentTimeTool(resolver, fixedClock("2026-08-16T01:30:00Z"))
    output, err := tool.Execute(context.Background(), json.RawMessage(`{"location":"Tokyo"}`), nil)
    if err != nil { t.Fatal(err) }
    got := decodeToolJSON[currentTimeResult](t, output)
    if got.LocalTime != "2026-08-16T10:30:00+09:00" || got.Date != "2026-08-16" || got.Weekday != "Sunday" {
        t.Fatalf("result = %#v", got)
    }
}
```

补充无结果、歧义、国家/行政区消歧、无效时区和 Context 取消测试。

- [ ] **Step 2: 运行测试确认 RED**

Run: `go test ./application/tool/chat -run 'TestCurrentTimeTool'`

Expected: FAIL，Tool 尚不存在。

- [ ] **Step 3: 实现当地时间 Tool**

导入 `_ "time/tzdata"`。输入 Schema 只允许 `location`、`country_code`、`admin1`；Definition 设置 `ParallelSafe:true`；成功输出 `status`、地点、RFC3339 `local_time`、`2006-01-02` 日期和英文 Weekday，地点失败复用 Task 3 的结构化正常状态。

```go
type currentTimeResult struct {
    Status string `json:"status"`
    Location locationView `json:"location"`
    LocalTime string `json:"local_time"`
    Date string `json:"date"`
    Weekday string `json:"weekday"`
}

now := tool.clock().In(zone)
result := currentTimeResult{
    Status: "ok", Location: locationToView(location),
    LocalTime: now.Format(time.RFC3339), Date: now.Format("2006-01-02"), Weekday: now.Weekday().String(),
}
return jsonOutput(result)
```

- [ ] **Step 4: 运行 Chat Tool 测试**

Run: `go test ./application/tool/chat`

Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add application/tool/chat/current_time.go application/tool/chat/current_time_test.go
git commit -m "feat: add local time chat tool"
```

### Task 5: `calculate` 纯算术 Tool

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Create: `application/tool/chat/calculate.go`
- Create: `application/tool/chat/calculate_test.go`

**Interfaces:**
- Consumes: `github.com/expr-lang/expr` 与 `github.com/expr-lang/expr/ast`
- Produces: `newCalculateTool() *calculateTool` 和 Tool Definition `calculate`

- [ ] **Step 1: 添加依赖并写算术白名单测试**

Run: `go get github.com/expr-lang/expr@v1.17.8`

表驱动测试使用字面结果覆盖 `1+2*3`、括号、小数、取模、幂、一元正负；拒绝空白、超过 256 字符、语法错误、除零、NaN/Inf、标识符、函数调用、成员访问、集合、字符串和条件表达式。

```go
func TestCalculateToolRejectsNonArithmeticAST(t *testing.T) {
    tool := newCalculateTool()
    for _, expression := range []string{
        `now()`, `foo`, `foo.bar`, `[1, 2]`, `{"x": 1}`, `true ? 1 : 2`, `"1"`,
    } {
        t.Run(expression, func(t *testing.T) {
            _, err := tool.Execute(context.Background(), mustJSON(map[string]any{"expression": expression}), nil)
            if pierrors.ErrorCodeOf(err) != pierrors.ErrorCodeToolInvalidArguments {
                t.Fatalf("error = %v", err)
            }
        })
    }
}

func mustJSON(value any) json.RawMessage {
    data, err := json.Marshal(value)
    if err != nil { panic(err) }
    return data
}
```

- [ ] **Step 2: 运行测试确认 RED**

Run: `go test ./application/tool/chat -run 'TestCalculateTool'`

Expected: FAIL，Tool 尚不存在。

- [ ] **Step 3: 实现 AST 访问器与有限数校验**

```go
type arithmeticValidator struct {
    err error
    integers map[ast.Node]*big.Int
}

var allowedBinary = map[string]bool{
    "+": true, "-": true, "*": true, "/": true, "%": true, "**": true,
}

func (v *arithmeticValidator) Visit(node *ast.Node) {
    if v.err != nil { return }
    switch current := (*node).(type) {
    case *ast.IntegerNode:
        v.integers[current] = big.NewInt(int64(current.Value))
    case *ast.FloatNode:
    case *ast.UnaryNode:
        if current.Operator != "+" && current.Operator != "-" {
            v.err = errors.New("unsupported unary operator")
            return
        }
        if value := v.integers[current.Node]; value != nil {
            value = new(big.Int).Set(value)
            if current.Operator == "-" { value.Neg(value) }
            v.recordInteger(current, value)
        }
    case *ast.BinaryNode:
        if !allowedBinary[current.Operator] {
            v.err = errors.New("unsupported binary operator")
            return
        }
        left, right := v.integers[current.Left], v.integers[current.Right]
        if left != nil && right != nil && current.Operator != "/" && current.Operator != "**" {
            value := new(big.Int)
            switch current.Operator {
            case "+": value.Add(left, right)
            case "-": value.Sub(left, right)
            case "*": value.Mul(left, right)
            case "%":
                if right.Sign() == 0 { v.err = errors.New("integer divide by zero"); return }
                value.Rem(left, right)
            }
            v.recordInteger(current, value)
        }
    default:
        v.err = fmt.Errorf("unsupported expression node %T", current)
    }
}

func (v *arithmeticValidator) recordInteger(node ast.Node, value *big.Int) {
    limit := new(big.Int).Lsh(big.NewInt(1), strconv.IntSize-1)
    minimum, maximum := new(big.Int).Neg(new(big.Int).Set(limit)), new(big.Int).Sub(limit, big.NewInt(1))
    if value.Cmp(minimum) < 0 || value.Cmp(maximum) > 0 {
        v.err = errors.New("integer overflow")
        return
    }
    v.integers[node] = value
}
```

用 `expr.Env(map[string]any{})`、`expr.DisableAllBuiltins()`、`expr.Optimize(false)`、`expr.MaxNodes(128)`、`expr.Patch(validator)` 和 `expr.AsFloat64()` 编译；初始化 `integers: make(map[ast.Node]*big.Int)`，先检查 `validator.err`，再用空 map 执行并断言结果是有限 `float64`。关闭 Expr 常量优化，确保每个整数中间结果都先经过 `big.Int` 原生位宽检查；`/` 和 `**` 按浮点执行并通过 NaN/Inf 检查，`%` 保留整数语义。结果使用以下稳定结构，Definition 设置 `ParallelSafe:true`：

```go
return jsonOutput(struct {
    Expression string `json:"expression"`
    Result float64 `json:"result"`
}{Expression: expression, Result: result})
```

所有解析、白名单和结果错误包装为 `tool_invalid_arguments`。

- [ ] **Step 4: 运行 Calculator 和全部 Chat Tool 测试**

Run: `go test ./application/tool/chat`

Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add go.mod go.sum application/tool/chat/calculate.go application/tool/chat/calculate_test.go
git commit -m "feat: add safe calculation chat tool"
```

### Task 6: Fx 注册与 Direct Loop 集成

**Files:**
- Create: `application/tool/chat/register.go`
- Create: `application/tool/chat/register_test.go`
- Create: `infrastructure/driver/openmeteo/register.go`
- Create: `infrastructure/driver/openmeteo/register_test.go`
- Modify: `application/web/register.go`
- Modify: `application/web/register_test.go`

**Interfaces:**
- Consumes: Task 1-5 的 Tool 构造函数、`pi.CoreRegister`、`pi.ReadOnlyToolsRegister`、Fx `group:"agent_tools"`
- Produces: `chat.Register`、`openmeteo.Register`
- Modifies: `application/web.agentRegister` 最终组合三个业务 Tool

- [ ] **Step 1: 写 Fx 图和 Tool 集合失败测试**

把现有 `TestAgentRegisterUsesDirectReadOnlyRuntime` 期望改为准确集合，并保留 Thinking 断言：

```go
want := []string{"calculate", "get_current_time", "get_weather", "read"}
if !slices.Equal(names, want) { t.Fatalf("Web Agent tools = %v, want %v", names, want) }
for _, forbidden := range []string{"apply_patch", "edit", "exec", "process", "write"} {
    if slices.Contains(names, forbidden) { t.Fatalf("Web Agent exposed %q", forbidden) }
}
if bool(thinking) { t.Fatal("Web Agent unexpectedly enables Thinking") }
```

把测试名同步改为 `TestAgentRegisterUsesDirectGeneralChatRuntime`。现有显式业务 Tool 测试仍保留 `course_query`，但期望集合更新为：

```go
want := []string{"calculate", "course_query", "get_current_time", "get_weather", "read"}
if !slices.Equal(names, want) { t.Fatalf("Web Agent tools = %v, want %v", names, want) }
```

`openmeteo/register_test.go` Populate 两个领域接口，并通过反射指针断言它们来自同一个 `*Client` 实例。

- [ ] **Step 2: 运行测试确认 RED**

Run: `go test ./application/tool/chat ./infrastructure/driver/openmeteo ./application/web -run 'Test.*Register|TestAgentRegisterUsesDirect'`

Expected: FAIL，Register 尚不存在且 Web 仍只有 `read`。

- [ ] **Step 3: 实现两个注册模块并装配 Web**

```go
// application/tool/chat/register.go
var Register = fx.Options(fx.Provide(
    newSystemClock,
    fx.Annotate(newWeatherTool, fx.As(new(ai.Tool)), fx.ResultTags(`group:"agent_tools"`)),
    fx.Annotate(newCurrentTimeTool, fx.As(new(ai.Tool)), fx.ResultTags(`group:"agent_tools"`)),
    fx.Annotate(newCalculateTool, fx.As(new(ai.Tool)), fx.ResultTags(`group:"agent_tools"`)),
))

// infrastructure/driver/openmeteo/register.go
var Register = fx.Options(fx.Provide(fx.Annotate(
    NewClient,
    fx.As(new(service.LocationResolver), new(service.WeatherProvider)),
)))
```

```go
var agentRegister = fx.Options(
    pi.CoreRegister,
    pi.ReadOnlyToolsRegister,
    chattools.Register,
    openmeteo.Register,
    fx.Supply(pi.ThinkingEnabled(false)),
)
```

- [ ] **Step 4: 写并运行真实 Loop 集成测试**

在 `application/tool/chat/register_test.go` 用脚本 Provider：第一次返回 `get_weather` ToolCall，第二次检查收到 `RoleTool` 且 JSON `status=ok` 后返回最终 Assistant。用真实 `runtime, err := pi.NewToolRuntime(pi.ToolRuntimeOptions{Tools: tools, Middlewares: pi.DefaultMiddlewareRegistrations()})` 和 `loop := pi.NewLoop(provider, pi.NewScheduler(runtime, 4), false)`，断言消息序列是 Assistant ToolCall、Tool Result、最终 Assistant，Provider 恰好调用 2 次且没有 Thinking 事件。另加 `ambiguous` fixture，断言歧义 JSON 能进入第二次 Provider 请求。

Run: `go test ./application/tool/chat ./infrastructure/driver/openmeteo ./application/web`

Expected: PASS。

- [ ] **Step 5: 确认 SDK 默认注册未变化**

Run: `go test ./pi -run 'TestReadOnlyToolsRegisterExposesOnlyRead|TestCodingToolsRegisterPreservesCompleteDefaultSet'`

Expected: PASS，`pi` 集合仍分别是 `read` 和六个 Coding Tool。

- [ ] **Step 6: 提交**

```bash
git add application/tool/chat/register.go application/tool/chat/register_test.go infrastructure/driver/openmeteo/register.go infrastructure/driver/openmeteo/register_test.go application/web/register.go application/web/register_test.go
git commit -m "feat: register general chat tools"
```

### Task 7: `weather-assistance` Skill

**Files:**
- Create: `workspaces/chat/skills/weather-assistance/SKILL.md`
- Modify: `application/web/workspace_test.go`
- Temporary only: `/tmp/go-reagent-skill-eval/weather-assistance/`

**Interfaces:**
- Consumes: Web Tool `get_weather`
- Produces: Workspace Skill `weather-assistance`

- [ ] **Step 1: RED 基线评测**

在 Skill 文件存在前，用 5 个独立、干净上下文的子代理运行等价请求：“明天北京天气怎么样，出门要不要带伞？”子代理只能读取 `workspaces/chat/AGENTS.md`，不得调用外部 API、数据库或看到期望答案。把五份原始回复分别保存为 `/tmp/go-reagent-skill-eval/weather-assistance/baseline-1.md` 到 `baseline-5.md`，记录是否虚构具体天气或未说明能力限制。

- [ ] **Step 2: 创建最小 Skill**

```markdown
---
name: weather-assistance
description: Use when the user asks about weather, temperature, rain, snow, umbrellas, clothing, outdoor activities, or forecasts.
---

# 天气协助

1. 缺少地点时，只询问地点。
2. 调用 `get_weather` 获取真实天气，不根据模型知识猜测。
3. `ambiguous` 时列出简短候选，请用户确认地点。
4. `not_found` 时请用户改用城市、国家或一级行政区描述。
5. 回答中明确解析地点和预报日期。
6. 穿衣、带伞和活动建议必须依据温度、降水概率和风力。
7. Tool 失败时说明暂时无法获取，不编造替代数据。
```

- [ ] **Step 3: GREEN 回归评测**

用 5 个新的独立子代理重复等价请求，给每个代理真实 Workspace 路径但不泄漏评判标准。确认代理先完整读取 `skills/weather-assistance/SKILL.md`，没有真实 Tool 结果时不编造；原始回复保存为 `green-1.md` 到 `green-5.md`。若出现新失败，只针对观察到的失败收紧 Skill，并重新跑 5 次。

- [ ] **Step 4: 增加默认 Workspace Skill 格式验证**

```go
func TestDefaultChatWorkspaceSkillsHaveNoDiagnostics(t *testing.T) {
    workspace := filepath.Join("..", "..", "workspaces", "chat")
    snapshot, err := skills.Discover(workspace)
    if err != nil { t.Fatal(err) }
    if diagnostics := snapshot.Diagnostics(); len(diagnostics) != 0 {
        t.Fatalf("Skill diagnostics = %#v", diagnostics)
    }
}
```

- [ ] **Step 5: 运行发现验证**

Run: `go test ./application/web -run 'TestDefaultChatWorkspaceSkillsHaveNoDiagnostics|TestDefaultChatWorkspacePromptDoesNotLoadRepositoryCodingIdentity'`

Expected: PASS，Prompt 仍为 Chat 身份且不泄漏仓库 Skill。

- [ ] **Step 6: 提交**

```bash
git add workspaces/chat/skills/weather-assistance/SKILL.md application/web/workspace_test.go
git commit -m "feat: add weather assistance skill"
```

### Task 8: `writing-assistance` Skill

**Files:**
- Create: `workspaces/chat/skills/writing-assistance/SKILL.md`
- Temporary only: `/tmp/go-reagent-skill-eval/writing-assistance/`

**Interfaces:**
- Consumes: 现有 Workspace Skill Discovery 与 `read`
- Produces: Workspace Skill `writing-assistance`

- [ ] **Step 1: RED 基线评测**

Skill 文件存在前，使用 5 个独立子代理运行：“请直接写一封给供应商的中文邮件，说明原定 8 月 20 日的交付改到 8 月 23 日，语气礼貌简洁。”保存原始回复，记录是否在信息充分时仍先输出计划、反复提问或虚构姓名/价格。

- [ ] **Step 2: 创建最小 Skill**

```markdown
---
name: writing-assistance
description: Use when the user asks to draft, rewrite, polish, shorten, or adjust the tone of an email, notice, copy, or report.
---

# 写作协助

1. 信息充分时直接给成稿，不先叙述写作计划。
2. 只有缺少受众、目的等关键条件时才询问一个问题。
3. 匹配用户要求的语言、语气、长度和格式。
4. 不虚构姓名、日期、价格或业务事实；缺失但必须出现的内容使用明确占位符。
5. 修改已有文本时保留用户未要求改变的内容。
```

- [ ] **Step 3: GREEN 回归评测**

用 5 个新子代理重复等价请求，确认完整读取该 Skill 后直接给成稿且不虚构；结果只写 `/tmp`。同时用一个无关问候验证不读取该 Skill。

- [ ] **Step 4: 运行 Workspace Skill 格式验证**

Run: `go test ./application/web -run TestDefaultChatWorkspaceSkillsHaveNoDiagnostics`

Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add workspaces/chat/skills/writing-assistance/SKILL.md
git commit -m "feat: add writing assistance skill"
```

### Task 9: `decision-support` Skill

**Files:**
- Create: `workspaces/chat/skills/decision-support/SKILL.md`
- Temporary only: `/tmp/go-reagent-skill-eval/decision-support/`

**Interfaces:**
- Consumes: 现有 Workspace Skill Discovery 与 `read`
- Produces: Workspace Skill `decision-support`

- [ ] **Step 1: RED 基线评测**

Skill 文件存在前，使用 5 个独立子代理运行：“我在 A 方案（便宜、两周交付）和 B 方案（贵 30%、三天交付）中选择，目标是下周上线，帮我比较并推荐。”保存原始回复，记录是否混淆已知事实、用户约束与推测。

- [ ] **Step 2: 创建最小 Skill**

```markdown
---
name: decision-support
description: Use when the user asks to compare, choose, rank, weigh tradeoffs, or recommend between options.
---

# 决策协助

1. 提取目标、硬约束和偏好。
2. 使用与目标相关的明确维度比较选项。
3. 区分用户提供的事实、用户偏好和推测。
4. 给出带条件的推荐，并说明主要代价。
5. 缺少实时数据 Tool 时明确限制，不把旧知识当作当前事实。
```

- [ ] **Step 3: GREEN 回归与组合验证**

用 5 个新子代理重复等价请求，确认读取 Skill 后围绕“下周上线”给出条件推荐。再验证“写一封比较 A/B 的邮件”只读取 decision 与 writing 两个必要 Skill，结果只写 `/tmp`。

- [ ] **Step 4: 运行 Workspace Skill 格式验证**

Run: `go test ./application/web -run TestDefaultChatWorkspaceSkillsHaveNoDiagnostics`

Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add workspaces/chat/skills/decision-support/SKILL.md
git commit -m "feat: add decision support skill"
```

### Task 10: `learning-explanation` Skill

**Files:**
- Create: `workspaces/chat/skills/learning-explanation/SKILL.md`
- Temporary only: `/tmp/go-reagent-skill-eval/learning-explanation/`

**Interfaces:**
- Consumes: 现有 Workspace Skill Discovery 与 `read`
- Produces: Workspace Skill `learning-explanation`

- [ ] **Step 1: RED 基线评测**

Skill 文件存在前，使用 5 个独立子代理运行：“用一个简单例子解释什么是复利。”保存原始回复，记录是否埋没核心答案、过度展开或强制测验/追问。

- [ ] **Step 2: 创建最小 Skill**

```markdown
---
name: learning-explanation
description: Use when the user asks for a concept explanation, tutoring, step-by-step teaching, examples, or practice.
---

# 学习讲解

1. 从用户表达推断水平，确实无法判断时才询问。
2. 先给核心答案，再分步骤说明。
3. 至少提供一个贴合问题的例子。
4. 简单问题保持简短，不强制测验或追问。
5. 用户表示没理解时更换解释角度，不重复原文。
```

- [ ] **Step 3: GREEN 回归与无关触发验证**

用 5 个新子代理重复等价请求，确认读取 Skill 后先给核心答案、包含一个例子且不强制追问。用普通翻译和简单总结各验证一次不读取此 Skill，结果只写 `/tmp`。

- [ ] **Step 4: 运行 Workspace Skill 格式验证**

Run: `go test ./application/web -run TestDefaultChatWorkspaceSkillsHaveNoDiagnostics`

Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add workspaces/chat/skills/learning-explanation/SKILL.md
git commit -m "feat: add learning explanation skill"
```

### Task 11: Chat Skill Catalog、文档与全量验证

**Files:**
- Modify: `application/web/workspace_test.go`
- Modify: `README.md`
- Modify: `docs/web-chat.md`

**Interfaces:**
- Consumes: 四个 Skill、三个 Chat Tool 和现有 `skills.Discover`/`read`
- Produces: 默认 Chat Workspace 与 Web 能力的回归保护和用户文档

- [ ] **Step 1: 写默认 Workspace Catalog 集成测试**

```go
func TestDefaultChatWorkspaceDiscoversOnlyGeneralChatSkills(t *testing.T) {
    workspace := filepath.Join("..", "..", "workspaces", "chat")
    snapshot, err := skills.Discover(workspace)
    if err != nil { t.Fatal(err) }
    summaries := snapshot.Skills()
    names := make([]string, len(summaries))
    for i, summary := range summaries { names[i] = summary.Name }
    want := []string{"decision-support", "learning-explanation", "weather-assistance", "writing-assistance"}
    if !slices.Equal(names, want) { t.Fatalf("skills = %v, want %v", names, want) }
    if len(snapshot.Diagnostics()) != 0 { t.Fatalf("diagnostics = %#v", snapshot.Diagnostics()) }
    for _, summary := range summaries {
        if summary.Location != "skills/"+summary.Name+"/SKILL.md" || summary.Source != skills.SourceWorkspace {
            t.Fatalf("summary = %#v", summary)
        }
    }
}
```

扩展现有 Prompt 测试：断言 Catalog 包含四个 name/location/description，但不包含每个 Skill Body 的唯一句子；通过真实 `agentRegister` 的 `read` Tool 逐个读取四个路径并断言全文可见；断言 `repository-development` 不出现。

- [ ] **Step 2: 运行 Catalog 与 Web 图测试**

Run: `go test ./application/web ./pi/harness/skills/...`

Expected: PASS。

- [ ] **Step 3: 更新 README 与 Web Chat 文档**

明确写入：

```text
Web 默认工具：calculate、get_current_time、get_weather、read。
天气数据来自 Open-Meteo，无需 API Key；重名地点会先要求消歧。
Web 不提供网页搜索、提醒、长期记忆、在线训练或 Coding 工具。
修改 Workspace Skill 在下一次 Run 生效；修改 Go Tool 需要重新构建并重启。
```

README 的“完整 SDK 六工具”章节继续只描述 `pi.Register`，不要把三个业务 Tool 写入 SDK 默认工具集合；并把“当前只有 read 并发安全”改为区分 SDK Coding 图和 Web Chat 图。

- [ ] **Step 4: 运行聚焦、全量、竞态和构建验证**

```bash
go test ./domain/... ./application/tool/... ./infrastructure/driver/openmeteo/... ./application/web/...
go test ./...
go test -race ./...
go build ./cmd/server
git diff --check
```

Expected: 所有命令退出码 0；测试不访问真实 Open-Meteo、不要求 MySQL 运行，不修改 `/tmp` 之外的评测数据。

- [ ] **Step 5: 检查最终能力与工作区边界**

```bash
git status --short
git diff --name-only 6310959..HEAD
```

确认没有 Migration、前端、HTTP Controller、Cookie、Conversation 或 `pi` 核心文件变更；`pi/recovery.go` 与 `pi/test/recovery_test.go` 仍是用户未提交改动且未被任何任务暂存。

- [ ] **Step 6: 提交文档和集成测试**

```bash
git add application/web/workspace_test.go README.md docs/web-chat.md
git commit -m "docs: describe general chat capabilities"
```
