package service

import (
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

func TestBandwagonQuotaEnforcementRolloverAndHistory(t *testing.T) {
	setupBulkDB(t)
	db := database.GetDB()
	svc := &InboundService{}
	now := time.Now().Unix() / 60 * 60
	if err := SaveBandwagonConfig(BandwagonConfig{Enabled: true, VEID: "123", APIKey: "test"}); err != nil {
		t.Fatal(err)
	}
	if err := applyBandwagonSnapshot(db, "123", bwgResponse(now+600, 0), now-600); err != nil {
		t.Fatal(err)
	}
	c := model.ClientRecord{Email: "cycle@test", UUID: "11111111-1111-1111-1111-111111111111", Enable: true, TotalGB: 100}
	if err := db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	ib := model.Inbound{UserId: 1, Tag: "cycle", Enable: true, Port: 33333, Protocol: model.VLESS, Settings: clientsSettings(t, []model.Client{{Email: c.Email, ID: c.UUID, Enable: true, TotalGB: 100}})}
	if err := db.Create(&ib).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ClientInbound{ClientId: c.Id, InboundId: ib.Id}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&xray.ClientTraffic{Email: c.Email, InboundId: ib.Id, Enable: true, Total: 100, Up: 900, Down: 100}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.BandwagonTraffic{VEID: "123", ClientID: c.Id, Minute: now, Up: 20, Down: 30}).Error; err != nil {
		t.Fatal(err)
	}
	batch := newTrafficMutationBatch()
	if err := svc.reconcileBandwagonQuotas(db, batch); err != nil {
		t.Fatal(err)
	}
	if _, n, _, err := svc.disableInvalidClients(db, batch); err != nil || n != 0 {
		t.Fatalf("old lifetime quota disabled current cycle: %d %v", n, err)
	}
	q := newClientQuery(db, time.Now().UnixMilli(), 0, 0)
	var depleted int64
	if err := q.from().Where(q.depletedExpr()).Count(&depleted).Error; err != nil || depleted != 0 {
		t.Fatalf("wrong summary %d %v", depleted, err)
	}
	items, err := q.pageRows(ClientPageParams{}, nil, 0, 10)
	if err != nil || len(items) != 1 || items[0].QuotaUsed == nil || *items[0].QuotaUsed != 50 {
		t.Fatalf("wrong page quota: %+v %v", items, err)
	}
	// Push current period exactly to its limit. Both upload and download count.
	if err := db.Model(&model.BandwagonTraffic{}).Where("client_id = ?", c.Id).Update("down", 80).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.reconcileBandwagonQuotas(db, batch); err != nil {
		t.Fatal(err)
	}
	if trafficOf(t, c.Email).Enable {
		t.Fatal("cycle quota did not block")
	}
	if len(batch.localPlans) != 1 || batch.localPlans[0].action != trafficRemoveUser {
		t.Fatal("runtime removal not planned")
	}
	if !recordEnableOf(t, &ClientService{}, c.Email) {
		t.Fatal("quota overwrote operator enable flag")
	}
	if err := svc.DelDepletedClients(-1); err != nil {
		t.Fatal(err)
	}
	var survivors int64
	db.Model(&model.ClientRecord{}).Where("id = ?", c.Id).Count(&survivors)
	if survivors != 1 {
		t.Fatal("renewable cycle account purged using lifetime counters")
	}
	if count, _, err := (&ClientService{}).DelDepleted(svc); err != nil || count != 0 {
		t.Fatal("bulk purge removed renewable cycle account", count, err)
	}
	// Confirm a new provider period. Previous records and lifetime counters survive.
	if err := applyBandwagonSnapshot(db, "123", bwgResponse(now+600+30*86400, 0), now+601); err != nil {
		t.Fatal(err)
	}
	batch = newTrafficMutationBatch()
	if err := svc.reconcileBandwagonQuotas(db, batch); err != nil {
		t.Fatal(err)
	}
	if !trafficOf(t, c.Email).Enable || len(batch.localPlans) != 1 || batch.localPlans[0].action != trafficAddUser {
		t.Fatal("new period failed to restore exhausted account")
	}
	if got := trafficOf(t, c.Email); got.Up != 900 || got.Down != 100 {
		t.Fatal("legacy counters changed")
	}
	var n int64
	db.Model(&model.BandwagonTraffic{}).Count(&n)
	if n != 1 {
		t.Fatal("history removed")
	}
	batch = newTrafficMutationBatch()
	if err := svc.reconcileBandwagonQuotas(db, batch); err != nil || len(batch.localPlans) != 0 {
		t.Fatal("rollover not idempotent", err)
	}
	// Manual and expired clients must never be revived by a retained block.
	for _, mode := range []string{"manual", "expired"} {
		db.Create(&model.BandwagonQuotaBlock{ClientID: c.Id, CycleID: 1})
		db.Model(&xray.ClientTraffic{}).Where("email = ?", c.Email).Update("enable", false)
		if mode == "manual" {
			db.Model(&c).Update("enable", false)
		} else {
			db.Model(&c).Updates(map[string]any{"enable": true, "expiry_time": 1})
		}
		batch = newTrafficMutationBatch()
		if err := svc.reconcileBandwagonQuotas(db, batch); err != nil {
			t.Fatal(err)
		}
		if trafficOf(t, c.Email).Enable || len(batch.localPlans) != 0 {
			t.Fatal("revived", mode)
		}
		db.Delete(&model.BandwagonQuotaBlock{}, "client_id = ?", c.Id)
	}
}
