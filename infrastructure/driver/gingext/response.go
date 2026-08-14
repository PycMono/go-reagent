package gingext

import (
	"errors"
	"net/http"

	ginsdk "github.com/PycMono/go-gin-sdk"
	commonerrors "github.com/PycMono/go-reagent/common/errors"
	"github.com/gin-gonic/gin"
)

// Send writes the repository's stable JSON envelope and HTTP status mapping.
func Send(c *gin.Context, data any, err error) {
	if data == nil {
		data = struct{}{}
	}
	body := &ginsdk.HTTPJSONBody{Code: ginsdk.StatusOK, Msg: "success", Data: data}
	status := http.StatusOK
	if err != nil {
		body.Data = struct{}{}
		var codeErr commonerrors.CodeError
		if errors.As(err, &codeErr) {
			body.Code = codeErr.Code()
			body.Msg = codeErr.Message()
			status = httpStatusForCode(codeErr.Code())
		} else {
			body.Code = commonerrors.ErrInternal.Code()
			body.Msg = commonerrors.ErrInternal.Message()
			status = http.StatusInternalServerError
		}
	}
	c.JSON(status, body)
}

func httpStatusForCode(code int) int {
	switch code {
	case commonerrors.ErrInvalidParam.Code():
		return http.StatusBadRequest
	case commonerrors.ErrUnauthorized.Code():
		return http.StatusUnauthorized
	case commonerrors.ErrForbidden.Code():
		return http.StatusForbidden
	case commonerrors.ErrUserNotFound.Code(), commonerrors.ErrNotFound.Code():
		return http.StatusNotFound
	case commonerrors.ErrConflict.Code():
		return http.StatusConflict
	case commonerrors.ErrRateLimited.Code():
		return http.StatusTooManyRequests
	default:
		return http.StatusInternalServerError
	}
}
