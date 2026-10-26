package service

import (
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

func TestRetiredClientLifecyclePreservesUsageAndAccess(t *testing.T) {
	setupBulkDB(t)
	db := database.GetDB()
	clients := []model.Client{
		{Email: "expired@local", ID: "11111111-1111-1111-1111-111111111111", Enable: true, ExpiryTime: 1, Reset: 1, TrafficReset: "daily"},
		{Email: "delayed@local", ID: "22222222-2222-2222-2222-222222222222", Enable: true, ExpiryTime: -86400000, ResetDay: 1},
	}
	ib := model.Inbound{Enable: true, Protocol: model.VLESS, Tag: "retired", Port: 34567, Settings: clientsSettings(t, clients)}
	if err := db.Create(&ib).Error; err != nil {
		t.Fatal(err)
	}
	svc := &InboundService{}
	if err := svc.clientService.SyncInbound(db, ib.Id, clients); err != nil {
		t.Fatal(err)
	}
	for _, c := range clients {
		if err := svc.AddClientStat(db, ib.Id, &c); err != nil {
			t.Fatal(err)
		}
	}
	_, _, _, _, _, err := svc.addTrafficLocked(nil, []*xray.ClientTraffic{{Email: clients[0].Email, Up: 10, Down: 20}, {Email: clients[1].Email, Up: 30, Down: 40}})
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range clients {
		tr := trafficOf(t, c.Email)
		if !tr.Enable || tr.ExpiryTime != c.ExpiryTime || tr.ResetCount != 0 || tr.Up != int64(10+i*20) || tr.Down != int64(20+i*20) {
			t.Fatalf("old lifecycle changed counters/access: %+v", tr)
		}
	}
	// Removed extension operations fail without mutating any stored state.
	if _, err := (&ClientService{}).ResetClientExpiryTimeByEmail(svc, clients[0].Email, 99999); err == nil {
		t.Fatal("expiry extension accepted")
	}
	if _, _, err := (&ClientService{}).BulkAdjust(svc, []string{clients[0].Email}, 30, 0, "", nil, ""); err == nil {
		t.Fatal("bulk renewal accepted")
	}
	if tr := trafficOf(t, clients[0].Email); tr.Up != 10 || tr.Down != 20 || tr.ExpiryTime != 1 {
		t.Fatal("rejected operation changed usage")
	}
}
