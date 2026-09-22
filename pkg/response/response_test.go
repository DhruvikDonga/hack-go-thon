package response

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"hack-go-thon/pkg/apperrors"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestResponseHelpers(t *testing.T) {
	t.Run("OK Response", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		OK(c, map[string]string{"message": "success"})

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}

		var resp Response
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		if !resp.Success {
			t.Errorf("expected resp.Success to be true")
		}
	})

	t.Run("Error Response", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		appErr := apperrors.NewBadRequest("invalid input", "field 'name' is required")
		Error(c, appErr)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}

		var resp Response
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		if resp.Success {
			t.Errorf("expected resp.Success to be false")
		}

		if resp.Error == nil || resp.Error.Code != apperrors.CodeBadRequest {
			t.Errorf("expected error code %s", apperrors.CodeBadRequest)
		}
	})
}
