package chat

import (
	"encoding/json"
	"net/http"

	"github.com/PycMono/go-context-sdk/bizctx"
	chatservice "github.com/PycMono/go-reagent/application/service/chat"
	"github.com/PycMono/go-reagent/common/dto"
	commonerrors "github.com/PycMono/go-reagent/common/errors"
	"github.com/PycMono/go-reagent/common/vo"
	"github.com/PycMono/go-reagent/infrastructure/driver/gingext"
	"github.com/gin-gonic/gin"
)

type Controller struct {
	service *chatservice.Service
}

func NewController(service *chatservice.Service) *Controller {
	return &Controller{service: service}
}

func (ctl *Controller) CreateConversation(c *gin.Context) {
	var param dto.CreateConversationDTO
	if err := c.ShouldBindJSON(&param); err != nil {
		gingext.Send(c, nil, commonerrors.ErrInvalidParam.Wrap(err))
		return
	}
	data, err := ctl.service.CreateConversation(c.Request.Context(), userID(c), param)
	gingext.Send(c, data, err)
}

func (ctl *Controller) ListAgentProfiles(c *gin.Context) {
	gingext.Send(c, ctl.service.ListAgentProfiles(), nil)
}

func (ctl *Controller) ListConversations(c *gin.Context) {
	var query dto.ListConversationsQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		gingext.Send(c, nil, commonerrors.ErrInvalidParam.Wrap(err))
		return
	}
	data, err := ctl.service.ListConversations(c.Request.Context(), userID(c), query)
	gingext.Send(c, data, err)
}

func (ctl *Controller) ListMessages(c *gin.Context) {
	var query dto.ListMessagesQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		gingext.Send(c, nil, commonerrors.ErrInvalidParam.Wrap(err))
		return
	}
	data, err := ctl.service.ListMessages(c.Request.Context(), userID(c), c.Param("id"), query)
	gingext.Send(c, data, err)
}

func (ctl *Controller) RenameConversation(c *gin.Context) {
	var param dto.RenameConversationDTO
	if err := c.ShouldBindJSON(&param); err != nil {
		gingext.Send(c, nil, commonerrors.ErrInvalidParam.Wrap(err))
		return
	}
	err := ctl.service.RenameConversation(c.Request.Context(), userID(c), c.Param("id"), param)
	gingext.Send(c, nil, err)
}

func (ctl *Controller) DeleteConversation(c *gin.Context) {
	err := ctl.service.DeleteConversation(c.Request.Context(), userID(c), c.Param("id"))
	gingext.Send(c, nil, err)
}

func (ctl *Controller) StartRun(c *gin.Context) {
	var param dto.StartRunDTO
	if err := c.ShouldBindJSON(&param); err != nil {
		gingext.Send(c, nil, commonerrors.ErrInvalidParam.Wrap(err))
		return
	}
	run, err := ctl.service.StartRun(c.Request.Context(), userID(c), c.Param("id"), param)
	if err != nil {
		gingext.Send(c, nil, err)
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	c.Writer.Flush()
	for {
		select {
		case event, ok := <-run.Events:
			if !ok {
				return
			}
			if err := writeSSE(c, event); err != nil {
				_ = ctl.service.CancelRun(c.Request.Context(), userID(c), c.Param("id"), run.ID)
				return
			}
		case <-c.Request.Context().Done():
			_ = ctl.service.CancelRun(c.Request.Context(), userID(c), c.Param("id"), run.ID)
			return
		}
	}
}

func (ctl *Controller) CancelRun(c *gin.Context) {
	err := ctl.service.CancelRun(c.Request.Context(), userID(c), c.Param("id"), c.Param("run_id"))
	gingext.Send(c, nil, err)
}

func writeSSE(c *gin.Context, event vo.RunEventVO) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := c.Writer.Write([]byte("event: " + string(event.Type) + "\n")); err != nil {
		return err
	}
	if _, err := c.Writer.Write([]byte("data: ")); err != nil {
		return err
	}
	if _, err := c.Writer.Write(data); err != nil {
		return err
	}
	if _, err := c.Writer.Write([]byte("\n\n")); err != nil {
		return err
	}
	c.Writer.Flush()
	return nil
}

func userID(c *gin.Context) string { return bizctx.GetUserID(c.Request.Context()) }
