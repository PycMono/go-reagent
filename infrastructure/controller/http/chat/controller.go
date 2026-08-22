package chat

import (
	"encoding/json"
	"net/http"

	"github.com/PycMono/go-context-sdk/bizctx"
	ginsdk "github.com/PycMono/go-gin-sdk"
	chatservice "github.com/PycMono/go-reagent/application/service/chat"
	"github.com/PycMono/go-reagent/common/dto"
	commonerrors "github.com/PycMono/go-reagent/common/errors"
	"github.com/PycMono/go-reagent/common/vo"
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
		ginsdk.Send(c, nil, commonerrors.ErrInvalidParam.Wrap(err), commonerrors.HTTPStatusForCode)
		return
	}
	data, err := ctl.service.CreateConversation(c.Request.Context(), userID(c), param)
	ginsdk.Send(c, data, err, commonerrors.HTTPStatusForCode)
}

func (ctl *Controller) ListAgentProfiles(c *gin.Context) {
	ginsdk.Send(c, ctl.service.ListAgentProfiles(), nil, commonerrors.HTTPStatusForCode)
}

func (ctl *Controller) ListConversations(c *gin.Context) {
	var query dto.ListConversationsQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		ginsdk.Send(c, nil, commonerrors.ErrInvalidParam.Wrap(err), commonerrors.HTTPStatusForCode)
		return
	}
	data, err := ctl.service.ListConversations(c.Request.Context(), userID(c), query)
	ginsdk.Send(c, data, err, commonerrors.HTTPStatusForCode)
}

func (ctl *Controller) ListMessages(c *gin.Context) {
	var query dto.ListMessagesQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		ginsdk.Send(c, nil, commonerrors.ErrInvalidParam.Wrap(err), commonerrors.HTTPStatusForCode)
		return
	}
	data, err := ctl.service.ListMessages(c.Request.Context(), userID(c), c.Param("id"), query)
	ginsdk.Send(c, data, err, commonerrors.HTTPStatusForCode)
}

func (ctl *Controller) RenameConversation(c *gin.Context) {
	var param dto.RenameConversationDTO
	if err := c.ShouldBindJSON(&param); err != nil {
		ginsdk.Send(c, nil, commonerrors.ErrInvalidParam.Wrap(err), commonerrors.HTTPStatusForCode)
		return
	}
	err := ctl.service.RenameConversation(c.Request.Context(), userID(c), c.Param("id"), param)
	ginsdk.Send(c, nil, err, commonerrors.HTTPStatusForCode)
}

func (ctl *Controller) DeleteConversation(c *gin.Context) {
	err := ctl.service.DeleteConversation(c.Request.Context(), userID(c), c.Param("id"))
	ginsdk.Send(c, nil, err, commonerrors.HTTPStatusForCode)
}

func (ctl *Controller) StartRun(c *gin.Context) {
	var param dto.StartRunDTO
	if err := c.ShouldBindJSON(&param); err != nil {
		ginsdk.Send(c, nil, commonerrors.ErrInvalidParam.Wrap(err), commonerrors.HTTPStatusForCode)
		return
	}
	run, err := ctl.service.StartRun(c.Request.Context(), userID(c), c.Param("id"), param)
	if err != nil {
		ginsdk.Send(c, nil, err, commonerrors.HTTPStatusForCode)
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
	ginsdk.Send(c, nil, err, commonerrors.HTTPStatusForCode)
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
