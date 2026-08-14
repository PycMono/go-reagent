package gingext

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	commonerrors "github.com/PycMono/go-reagent/common/errors"
	"github.com/gin-gonic/gin"
)

func TestSendMapsSuccessAndCodedErrors(t *testing.T) {
	tests := []struct {
		name       string
		data       any
		err        error
		wantStatus int
		wantCode   int
		wantMsg    string
	}{
		{name: "success", data: map[string]string{"id": "chat"}, wantStatus: 200, wantCode: 0, wantMsg: "success"},
		{name: "invalid", err: commonerrors.ErrInvalidParam, wantStatus: 400, wantCode: 10001, wantMsg: "invalid parameter"},
		{name: "not found", err: commonerrors.ErrNotFound, wantStatus: 404, wantCode: 10006, wantMsg: "resource not found"},
		{name: "conflict", err: commonerrors.ErrConflict, wantStatus: 409, wantCode: 10007, wantMsg: "resource conflict"},
		{name: "unknown", err: errors.New("secret detail"), wantStatus: 500, wantCode: 10009, wantMsg: "internal server error"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(response)
			Send(ctx, test.data, test.err)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d", response.Code)
			}
			var body struct {
				Code int             `json:"code"`
				Msg  string          `json:"msg"`
				Data json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Code != test.wantCode || body.Msg != test.wantMsg {
				t.Fatalf("body = %#v", body)
			}
			if test.name == "success" && string(body.Data) != `{"id":"chat"}` {
				t.Fatalf("success data = %s", body.Data)
			}
			if test.name == "unknown" && string(response.Body.Bytes()) == "secret detail" {
				t.Fatal("unknown error detail leaked")
			}
		})
	}
}

func TestHTTPStatusMappingIncludesAuthorizationAndRateLimits(t *testing.T) {
	for err, status := range map[error]int{
		commonerrors.ErrUnauthorized: http.StatusUnauthorized,
		commonerrors.ErrForbidden:    http.StatusForbidden,
		commonerrors.ErrRateLimited:  http.StatusTooManyRequests,
	} {
		response := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(response)
		Send(ctx, nil, err)
		if response.Code != status {
			t.Errorf("%v status = %d, want %d", err, response.Code, status)
		}
	}
}
