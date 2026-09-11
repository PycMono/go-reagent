package agenttraining

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/PycMono/go-reagent/pi"
	"strconv"
	"strings"
	"sync"

	"github.com/PycMono/go-reagent/application/identity"
	"github.com/PycMono/go-reagent/application/service/agenttraining"
	"github.com/PycMono/go-reagent/common/dto"
	ce "github.com/PycMono/go-reagent/common/errors"
	"github.com/PycMono/go-reagent/config"
	agentctl "github.com/PycMono/go-reagent/infrastructure/controller/http/agent"
	"github.com/gin-gonic/gin"
)

func RegisterRoutes(r *gin.Engine, service *agenttraining.Service, cfg *config.Config) {
	if !cfg.Identity.AdminEnabled() {
		return
	}
	admin := r.Group("/api/v1", agentctl.ManagementGuard())
	admin.POST("/training-sessions/:trainingID/preview", func(c *gin.Context) {
		var in dto.TrainingPreviewDTO
		if err := agentctl.Decode(c, &in); err != nil {
			agentctl.Send(c, nil, err)
			return
		}
		p, _ := identity.Require(c.Request.Context())
		content, err := service.Preview(c.Request.Context(), p, c.Param("trainingID"), in)
		agentctl.Send(c, gin.H{"content": content}, err)
	})
	// Authentication and strict request decoding are shared with Agent APIs;
	// all ownership and state transitions stay in the application service.
	admin.POST("/agents/:agentID/training-sessions", func(c *gin.Context) {
		var in dto.CreateTrainingDTO
		if err := agentctl.Decode(c, &in); err != nil {
			agentctl.Send(c, nil, err)
			return
		}
		p, _ := identity.Require(c.Request.Context())
		data, err := service.Create(c.Request.Context(), p, c.Param("agentID"), in)
		agentctl.Send(c, data, err)
	})
	admin.GET("/agents/:agentID/training-sessions", func(c *gin.Context) {
		limit, err := pageLimit(c)
		if err != nil {
			agentctl.Send(c, nil, err)
			return
		}
		p, _ := identity.Require(c.Request.Context())
		items, next, err := service.List(c.Request.Context(), p, c.Param("agentID"), c.Query("cursor"), limit)
		agentctl.Send(c, gin.H{"items": items, "next_cursor": next}, err)
	})
	admin.GET("/training-sessions/:trainingID", func(c *gin.Context) {
		p, _ := identity.Require(c.Request.Context())
		data, err := service.Get(c.Request.Context(), p, c.Param("trainingID"))
		agentctl.Send(c, data, err)
	})
	admin.GET("/training-sessions/:trainingID/messages", func(c *gin.Context) {
		limit, err := pageLimit(c)
		if err != nil {
			agentctl.Send(c, nil, err)
			return
		}
		var before uint64
		if raw, ok := c.GetQuery("before"); ok {
			before, err = strconv.ParseUint(raw, 10, 64)
			if err != nil {
				agentctl.Send(c, nil, ce.ErrInvalidParam)
				return
			}
		}
		p, _ := identity.Require(c.Request.Context())
		messages, err := service.Messages(c.Request.Context(), p, c.Param("trainingID"), before, limit)
		items := []gin.H{}
		for _, m := range messages {
			items = append(items, gin.H{"id": m.ID, "role": m.Role, "content": m.Payload.Content, "turn_version": strconv.FormatUint(m.TurnVersion, 10), "tool_call_id": m.Payload.ToolCallID, "tool_name": m.Payload.ToolName, "is_error": m.Payload.IsError})
		}
		agentctl.Send(c, gin.H{"items": items}, err)
	})
	admin.GET("/training-sessions/:trainingID/diff", func(c *gin.Context) {
		p, _ := identity.Require(c.Request.Context())
		data, err := service.Diff(c.Request.Context(), p, c.Param("trainingID"), 1<<20)
		agentctl.Send(c, data, err)
	})
	admin.GET("/training-sessions/:trainingID/checkpoints", func(c *gin.Context) {
		limit, err := pageLimit(c)
		if err != nil {
			agentctl.Send(c, nil, err)
			return
		}
		p, _ := identity.Require(c.Request.Context())
		items, next, err := service.Checkpoints(c.Request.Context(), p, c.Param("trainingID"), c.Query("cursor"), limit)
		agentctl.Send(c, gin.H{"items": items, "next_cursor": next}, err)
	})
	admin.POST("/training-sessions/:trainingID/runs", func(c *gin.Context) {
		var in dto.TrainingRunDTO
		if err := agentctl.Decode(c, &in); err != nil {
			agentctl.Send(c, nil, err)
			return
		}
		p, _ := identity.Require(c.Request.Context())
		if !strings.Contains(c.GetHeader("Accept"), "text/event-stream") {
			data, err := service.Run(c.Request.Context(), p, c.Param("trainingID"), in)
			agentctl.Send(c, data, err)
			return
		}
		ctx, cancel := context.WithCancel(c.Request.Context())
		defer cancel()
		stream := &trainingStream{c: c, cancel: cancel}
		data, err := service.Run(ctx, p, c.Param("trainingID"), in, stream)
		if !stream.started && err != nil {
			agentctl.Send(c, nil, err)
			return
		}
		if err != nil {
			message := "训练未完成，请查看已保存的结果和候选改动。"
			if biz, ok := ce.AsBizError(err); ok {
				message = biz.Message()
			}
			stream.send("run.failed", gin.H{"type": "run.failed", "message": message, "session": data})
		} else {
			stream.send("run.completed", gin.H{"type": "run.completed", "session": data})
		}
	})
	admin.PATCH("/training-sessions/:trainingID/config", func(c *gin.Context) {
		var in dto.TrainingConfigDTO
		if err := agentctl.Decode(c, &in); err != nil {
			agentctl.Send(c, nil, err)
			return
		}
		p, _ := identity.Require(c.Request.Context())
		data, err := service.PatchConfig(c.Request.Context(), p, c.Param("trainingID"), in)
		agentctl.Send(c, data, err)
	})
	admin.POST("/training-sessions/:trainingID/publish", func(c *gin.Context) {
		var in dto.TrainingPublishDTO
		if err := agentctl.Decode(c, &in); err != nil {
			agentctl.Send(c, nil, err)
			return
		}
		p, _ := identity.Require(c.Request.Context())
		data, err := service.Publish(c.Request.Context(), p, c.Param("trainingID"), in)
		agentctl.Send(c, data, err)
	})
	for _, action := range []string{"validate", "cancel", "checkpoints/:checkpointID/restore"} {
		admin.POST("/training-sessions/:trainingID/"+action, func(c *gin.Context) {
			var in dto.TrainingMutationDTO
			if err := agentctl.Decode(c, &in); err != nil {
				agentctl.Send(c, nil, err)
				return
			}
			p, _ := identity.Require(c.Request.Context())
			ctx := c.Request.Context()
			id := c.Param("trainingID")
			switch action {
			case "validate":
				data, err := service.Validate(ctx, p, id, in)
				agentctl.Send(c, data, err)
			case "cancel":
				data, err := service.Cancel(ctx, p, id, in)
				agentctl.Send(c, data, err)
			default:
				data, err := service.Restore(ctx, p, id, c.Param("checkpointID"), in)
				agentctl.Send(c, data, err)
			}
		})
	}
}

func pageLimit(c *gin.Context) (int, error) {
	if raw, ok := c.GetQuery("limit"); ok {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			return 0, ce.ErrInvalidParam
		}
		return n, nil
	}
	return 20, nil
}

// trainingStream forwards events synchronously; the mutex serializes parallel tool events.
// A disconnected client cancels execution; Service.Run still finalizes its checkpoint and ledger.
type trainingStream struct {
	c       *gin.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	started bool
	failed  bool
}

func (s *trainingStream) OnEvent(_ context.Context, event pi.AgentEvent) {
	s.send(string(event.Type), event)
}
func (s *trainingStream) send(kind string, value any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failed {
		return
	}
	payload, err := json.Marshal(value)
	if err != nil {
		s.failed = true
		s.cancel()
		return
	}
	if !s.started {
		s.c.Header("Content-Type", "text/event-stream; charset=utf-8")
		s.c.Header("Cache-Control", "no-cache")
		s.c.Header("X-Accel-Buffering", "no")
		s.started = true
	}
	if _, err = fmt.Fprintf(s.c.Writer, "event: %s\ndata: %s\n\n", kind, payload); err != nil {
		s.failed = true
		s.cancel()
		return
	}
	s.c.Writer.Flush()
}
