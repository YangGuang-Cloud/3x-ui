package service

import (
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func seedGlobalTraffic(t *testing.T, email string, up, down int64) {
	t.Helper()
	if err := database.GetDB().Create(&model.ClientGlobalTraffic{
		MasterGuid: "peer-" + email, Email: email, Up: up, Down: down,
	}).Error; err != nil {
		t.Fatalf("seed client_global_traffics: %v", err)
	}
}

func countGlobalTraffic(t *testing.T, email string) int64 {
	t.Helper()
	var n int64
	if err := database.GetDB().Model(&model.ClientGlobalTraffic{}).
		Where("email = ?", email).Count(&n).Error; err != nil {
		t.Fatalf("count client_global_traffics: %v", err)
	}
	return n
}

// A capped catch-up keeps its counters and stays expired, so its cross-panel
// rows still describe a window this client has not spent.

// The counterpart: a real renewal must drop the rows, or the stale pushed
// totals re-deplete the fresh window at once.
