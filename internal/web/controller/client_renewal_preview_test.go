package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestClientRenewalPreviewRetired(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	(&ClientController{}).renewalPreview(c)
	if w.Code != 410 {
		t.Fatalf("status=%d", w.Code)
	}
}
