package agent

import (
	"github.com/PycMono/go-reagent/config"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
)

func TestPublicationRoutesHiddenInAnonymousMode(t *testing.T) {
	r := gin.New()
	RegisterPublicationRoutes(r, nil, &config.Config{})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/agents/a/model-config-releases", nil))
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
}
