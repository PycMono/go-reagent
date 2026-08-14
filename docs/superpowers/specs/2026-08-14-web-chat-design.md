# Web Chat Design

## Goal

Add a long-running local Web chat service to go-reagent. Each browser is an anonymous user identified by an HTTP-only cookie. Users can create, search, rename, delete, and reopen conversations; send messages through the existing conversation and pi runtimes; inspect complete persisted user, assistant, and tool messages; observe live run status over SSE; and stop an active run.

The Web feature must not modify any file under `pi/`. It consumes the existing public `conversation.Runner`, `pi.Reporter`, `pi.RunResult`, and context cancellation contracts.

## Scope

The first version includes:

- a dedicated `cmd/server` Web entry point while preserving the existing one-shot `cmd/reagent` CLI;
- Gin and `go-gin-sdk` HTTP lifecycle wiring;
- Go `html/template` pages and disk-served static assets following the local `micro-framework` conventions;
- anonymous browser identity through a `reagent_visitor` cookie;
- conversation create, search, list, rename, and delete operations;
- paginated complete message history, including tool calls and tool results;
- persisted runs through the existing `conversation.Runner` and MySQL repository;
- SSE events for thinking, tool activity, completion, and failure;
- one active run per conversation and explicit cancellation;
- responsive desktop and mobile chat layouts.

The first version excludes:

- login or account recovery;
- file upload;
- preset prompts;
- regenerate and historical-message editing;
- token-level model streaming;
- Node, React, Vue, or another frontend build tool;
- multi-instance distributed run coordination;
- changes to `pi/`.

## Reference and Compatibility Rules

New Web code follows the structure and conventions of the sibling `micro-framework` repository:

- HTTP controllers live under `infrastructure/controller/http`;
- Gin and HTTP server construction live under `infrastructure/driver/gingext`;
- request DTOs and response VOs live under `common/dto` and `common/vo`;
- application orchestration lives under `application/service`;
- repository interfaces stay in `domain/repository`, with GORM implementations in `infrastructure/persistence`;
- every layer exposes Fx registration through `register.go` and `fx.Options`;
- pages use Go templates arranged as layouts, partials, components, and pages;
- Vanilla JavaScript and CSS are served from `frontend/static` without a build step;
- JSON responses use the `gingext.Send` `{code,msg,data}` envelope.

Existing go-reagent code is not moved merely to match the reference repository. In particular, the existing root `config`, `conversation`, domain entity, repository, and persistence packages remain in place. The Web layer adapts to them without unrelated restructuring.

Unlike `micro-framework`, go-reagent continues to use explicit SQL migrations. It must not introduce GORM `AutoMigrate`.

## Architecture

```text
Browser
  |- Go Template page + Vanilla JavaScript + CSS
  |- reagent_visitor cookie
  `- JSON requests and one streamed run response
         |
Gin HTTP controllers
         |
application/service/chat
  |- conversation management
  |- message queries
  |- active-run ownership and cancellation
  `- pi events to SSE events
         |
existing conversation.Runner
         |
existing pi.Runner + local tools
         |
existing MySQL conversation repository
```

`cmd/server` builds a long-lived Fx graph that does not construct or invoke the one-shot `application.AgentRunner`. `cmd/reagent` and its lifecycle remain unchanged.

The Web service requires `conversation.enabled=true`. It fails during startup when persistence is disabled because durable conversation history is a feature requirement, not an optional Web mode.

## Planned File Layout

```text
cmd/server/main.go

common/dto/chat.go
common/vo/chat.go

application/service/chat/register.go
application/service/chat/service.go
application/service/chat/run_manager.go
application/service/chat/reporter.go

infrastructure/controller/register.go
infrastructure/controller/http/register.go
infrastructure/controller/http/chat/controller.go
infrastructure/controller/http/page/controller.go
infrastructure/controller/http/page/renderer.go

infrastructure/driver/gingext/gingext.go
infrastructure/driver/gingext/response.go

infrastructure/middleware/visitor.go
infrastructure/middleware/same_origin.go

frontend/templates/layouts/base.html
frontend/templates/partials/*.html
frontend/templates/components/*.html
frontend/templates/pages/chat.html
frontend/static/css/pages/chat.css
frontend/static/js/pages/chat.js
```

Existing conversation entity, repository, persistence, config, registration, documentation, and test files are extended where their current responsibility requires it.

## Dependencies

The Web graph uses the same versions as the local reference implementation unless Go module resolution requires a compatible newer version:

- `github.com/PycMono/go-gin-sdk` v0.0.6;
- `github.com/PycMono/go-context-sdk` v1.0.2 for business user context;
- `github.com/gin-gonic/gin` v1.10.0.

No frontend package manager or generated bundle is introduced.

## Configuration

Add an HTTP section to the existing configuration:

```json
{
  "http": {
    "host": "127.0.0.1",
    "port": "8080",
    "read_timeout": 30,
    "write_timeout": 0,
    "secure_cookies": false
  }
}
```

The default host is `127.0.0.1` because the pi runtime exposes local file and process tools. A zero write timeout permits long-running SSE responses. HTTP host, port, and timeout values are normalized and validated during configuration loading.

The supplied MySQL configuration targets database `harness` on `127.0.0.1:3306`. Web startup also requires:

```json
{
  "conversation": {
    "enabled": true,
    "history_message_limit": 100
  }
}
```

## Data Model and Migration

Existing normalized tables remain authoritative:

- `agent_conversations` stores conversation ownership and metadata;
- `agent_messages` stores ordered user, assistant, and tool messages;
- `agent_model_invocations` stores model usage and cost records.

Add `migrations/0003_web_chat.up.sql` with one new conversation column:

```sql
ALTER TABLE agent_conversations
    ADD COLUMN name VARCHAR(255) NOT NULL DEFAULT 'Untitled Chat';
```

The Go conversation entity gains a matching `Name` field. No last-message preview or message-count cache columns are added. Message totals are computed from `agent_messages` for the current conversation page. The list response does not include a last-message preview.

The first successfully persisted turn assigns a default name from the first non-empty user message, truncated safely to the configured maximum length. Later automatic runs do not overwrite it. The rename operation is the only subsequent way to modify the name.

Deleting an owned conversation is a hard delete. Existing foreign keys cascade to its messages and model invocation records. The UI must require confirmation before issuing the delete request.

## Anonymous Browser Identity

The visitor middleware reads `reagent_visitor`. When missing or invalid, it generates a cryptographically random opaque identifier, stores it in a cookie, and injects it as the business user ID through `go-context-sdk/bizctx`.

Cookie properties are:

- `HttpOnly`;
- `SameSite=Lax`;
- `Path=/`;
- one-year maximum age;
- `Secure` when `http.secure_cookies=true`.

Clients never submit `user_id`. Every conversation and message operation applies both the public conversation ID and the cookie-derived user ID. A conversation belonging to another visitor is indistinguishable from a missing conversation and returns HTTP 404.

Clearing the cookie creates a new anonymous identity. The first version provides no mechanism to recover conversations associated with the previous cookie.

## HTTP API

All JSON APIs live under `/api/v1`, following `micro-framework` route grouping conventions.

### Page and Health Routes

| Method | Route | Purpose |
| --- | --- | --- |
| `GET` | `/` | Render the chat page |
| `GET` | `/static/*` | Serve frontend assets from disk |
| `GET` | `/health` | Report service liveness |

### Conversation Routes

| Method | Route | Purpose |
| --- | --- | --- |
| `POST` | `/api/v1/conversations` | Create an empty owned conversation |
| `GET` | `/api/v1/conversations` | Search and cursor-page owned conversations |
| `PATCH` | `/api/v1/conversations/:id` | Rename an owned conversation |
| `DELETE` | `/api/v1/conversations/:id` | Delete an owned conversation |

The list response item is:

```json
{
  "id": "conversation-id",
  "name": "Help me inspect this project",
  "message_total": 12,
  "created_at": "2026-08-14T10:00:00+08:00",
  "updated_at": "2026-08-14T10:05:20+08:00"
}
```

The list omits message content. Selecting an item loads the complete detailed message API.

### Message and Run Routes

| Method | Route | Purpose |
| --- | --- | --- |
| `GET` | `/api/v1/conversations/:id/messages` | Cursor-page complete ordered messages |
| `POST` | `/api/v1/conversations/:id/runs` | Send a user message and stream run events |
| `POST` | `/api/v1/conversations/:id/runs/:run_id/cancel` | Cancel the active run |

The message VO includes:

- message ID;
- turn version and ordinal;
- run ID;
- role;
- complete text content blocks;
- tool call IDs, names, and JSON arguments;
- tool result correlation ID and tool name;
- tool error state;
- creation time.

Conversation and message list endpoints use bounded cursor pagination. The frontend loads older messages on demand while preserving chronological display order.

## Run and SSE Contract

The run endpoint accepts one user text message as JSON. Its response uses `Content-Type: text/event-stream`, remains open while the existing conversation runner executes, and closes after a terminal event.

Supported event names are:

- `run.started` with the generated run ID;
- `agent.thinking`;
- `tool.started` with tool name and arguments;
- `tool.updated` with bounded incremental tool output;
- `tool.completed` with result or error metadata;
- `message.completed` with the final assistant text;
- `run.failed` with a safe error category and message;
- `run.completed`.

The current provider contract is synchronous, so `message.completed` contains the final text once. The feature does not simulate token streaming and does not change provider or pi loop interfaces.

The application run manager permits one active run per conversation in the current process. A second run returns HTTP 409. It maps `(user ID, conversation ID, run ID)` to a cancel function, removes terminal runs, and rejects cancellation by another visitor. Database optimistic locking remains the final protection against cross-process overlap.

The stop action invokes the cancellation endpoint before aborting the browser response reader. Request-context cancellation also propagates to pi when the browser disconnects. After success, failure, or cancellation, the frontend reloads persisted messages so the UI reflects database truth and the existing partial-result persistence rules.

## Frontend Design

The page reuses the local `micro-framework` AI Chat template, CSS vocabulary, Vanilla JavaScript module style, and project-selector interaction, adapted to the new API and normalized database model.

The left conversation panel provides:

- new chat;
- name search;
- Today, Yesterday, This Week, and Earlier groups;
- conversation selection;
- rename and confirmed delete actions;
- a mobile drawer presentation.

The main panel provides:

- current conversation name;
- paginated complete message history;
- right-aligned user message bubbles;
- assistant response text;
- collapsible assistant tool-call cards;
- collapsible tool-result output with clear error styling;
- thinking and active-tool status;
- automatic scroll management;
- a fixed composer where Enter sends and Shift+Enter inserts a line break;
- a send button that becomes a stop button while running.

The adapted page removes the reference implementation's localStorage conversation authority, file upload, preset prompts, regenerate action, and double-sidebar project layout. MySQL is the sole conversation authority for this Web service.

## Error Handling

JSON endpoints use the standard response envelope and meaningful HTTP statuses:

- 400 for malformed or invalid input;
- 404 for missing or unowned conversations;
- 409 for an already active run or optimistic-lock conflict;
- 500 for unexpected internal failures.

After SSE headers have been written, failures are expressed through `run.failed`, followed by stream termination. Client-facing errors distinguish validation, conversation conflict, model generation, tool failure, cancellation, and network failure without disclosing API keys, authorization headers, provider configuration, or hidden model traces.

Tool failures are both shown in the live run UI and reloaded from persisted message details when the existing runner stores them.

## Security

The Web server binds to loopback by default. It does not install permissive CORS. State-changing requests validate same-origin `Origin` or `Referer` information in addition to the Lax cookie policy.

This restriction is mandatory because a chat request can cause pi to invoke `read`, `write`, `edit`, `exec`, and `process` against the host workspace. Binding to `0.0.0.0` or another non-loopback address is outside the safe default and requires a future authentication and tool-authorization design.

## Testing

Implementation follows test-driven development. Automated coverage includes:

- visitor cookie creation, reuse, flags, and browser isolation;
- same-origin enforcement;
- conversation creation, search, cursor pagination, rename, and delete;
- ownership filtering and 404 behavior;
- message order and complete VO mapping;
- tool-call and tool-result detail mapping;
- message-total aggregation without denormalized columns;
- first-turn automatic naming;
- one-run-per-conversation conflict behavior;
- run cancellation and context propagation;
- SSE event order and JSON encoding;
- SQL behavior and transaction compatibility;
- Go template parsing and rendering;
- Fx Web graph construction and lifecycle.

Fresh completion verification runs:

```bash
go test ./...
go test -race ./...
go build ./cmd/server
```

With the supplied MySQL and a configured real model, browser verification covers:

- anonymous visitor creation;
- successful model chat through pi;
- live thinking and tool activity;
- complete persisted messages and invocation ledger rows;
- history restoration after refresh;
- browser-to-browser isolation;
- rename, delete, and stop actions;
- desktop and mobile layout behavior.

If MySQL or a real model is unavailable, the unverified integration steps and their environmental blocker must be reported explicitly.

## Acceptance Criteria

The feature is accepted when:

1. `cmd/server` starts a loopback Gin service without triggering the one-shot CLI agent task.
2. `/` renders the adapted Go-template chat page and static assets without a Node build.
3. Each browser receives an isolated anonymous user identity.
4. Conversation CRUD and detailed message history operate only on that identity's rows.
5. A sent message runs through the unchanged pi directory and existing conversation persistence path.
6. Thinking and tool events appear live, and the final assistant response appears when complete.
7. Stop cancels the active run and releases the per-conversation run slot.
8. Refreshing the page restores MySQL-backed conversations and complete message details.
9. Rename and confirmed delete behave consistently in the UI and database.
10. All automated tests, the race-enabled suite, and the server build pass, or any environment-only integration gap is documented precisely.
