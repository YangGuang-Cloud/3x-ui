package service

import (
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

const reenableDay = int64(24 * 60 * 60 * 1000)

func recordEnableOf(t *testing.T, svc *ClientService, email string) bool {
	t.Helper()
	rec, err := svc.GetRecordByEmail(nil, email)
	if err != nil {
		t.Fatalf("GetRecordByEmail(%q): %v", email, err)
	}
	return rec.Enable
}

func forceRecordDisabled(t *testing.T, svc *ClientService, email string) {
	t.Helper()
	if err := database.GetDB().Model(&model.ClientRecord{}).
		Where("email = ?", email).
		UpdateColumn("enable", false).Error; err != nil {
		t.Fatalf("force record disabled %q: %v", email, err)
	}
	if recordEnableOf(t, svc, email) {
		t.Fatalf("setup: record %q should start disabled", email)
	}
}

func jsonClientEnable(t *testing.T, inboundSvc *InboundService, inboundId int, email string) bool {
	t.Helper()
	ib, err := inboundSvc.GetInbound(inboundId)
	if err != nil {
		t.Fatalf("GetInbound(%d): %v", inboundId, err)
	}
	clients, err := inboundSvc.GetClients(ib)
	if err != nil {
		t.Fatalf("GetClients(%d): %v", inboundId, err)
	}
	for _, c := range clients {
		if c.Email == email {
			return c.Enable
		}
	}
	t.Fatalf("client %q not found in inbound %d settings JSON", email, inboundId)
	return false
}

func assertEnableEverywhere(t *testing.T, svc *ClientService, inboundSvc *InboundService, inboundId int, email string, want bool) {
	t.Helper()
	if got := trafficOf(t, email).Enable; got != want {
		t.Fatalf("%s: client_traffics.enable = %v, want %v", email, got, want)
	}
	if got := recordEnableOf(t, svc, email); got != want {
		t.Fatalf("%s: client_records.enable = %v, want %v", email, got, want)
	}
	if got := jsonClientEnable(t, inboundSvc, inboundId, email); got != want {
		t.Fatalf("%s: inbound JSON enable = %v, want %v", email, got, want)
	}
}

func seedLocalDisabledClient(t *testing.T, svc *ClientService, port int, stream, email string, total, expiry, up, down int64) *model.Inbound {
	t.Helper()
	c := model.Client{
		Email:      email,
		ID:         "11111111-1111-1111-1111-111111111111",
		SubID:      email,
		Enable:     false,
		TotalGB:    total,
		ExpiryTime: expiry,
	}
	var ib *model.Inbound
	if stream == "" {
		ib = mkInbound(t, port, model.VLESS, clientsSettings(t, []model.Client{c}))
	} else {
		ib = mkInboundStream(t, port, model.VLESS, clientsSettings(t, []model.Client{c}), stream)
	}
	if err := svc.SyncInbound(nil, ib.Id, []model.Client{c}); err != nil {
		t.Fatalf("seed linkage: %v", err)
	}
	mkTraffic(t, ib.Id, email, up, down, total, expiry, false)
	forceRecordDisabled(t, svc, email)
	return ib
}

func TestBulkAdjust_ReenablesOverQuota_WhenAddBytesClearsQuota(t *testing.T) {
	setupBulkDB(t)
	svc := &ClientService{}
	inboundSvc := &InboundService{}

	email := "q@x"
	ib := seedLocalDisabledClient(t, svc, 52004, "", email, 100, 0, 60, 40)

	res, _, err := svc.BulkAdjust(inboundSvc, []string{email}, 0, 200, "", nil, "")
	if err != nil {
		t.Fatalf("BulkAdjust: %v", err)
	}
	if res.Adjusted != 1 {
		t.Fatalf("expected 1 adjusted, got %d (skipped=%v)", res.Adjusted, res.Skipped)
	}
	assertEnableEverywhere(t, svc, inboundSvc, ib.Id, email, true)
	if got := trafficOf(t, email).Total; got != 300 {
		t.Fatalf("%s: total = %d, want 300", email, got)
	}
}

func TestBulkAdjust_QuotaReductionBelowZeroSkipsInsteadOfUnlimited(t *testing.T) {
	setupBulkDB(t)
	svc := &ClientService{}
	inboundSvc := &InboundService{}

	email := "qr@x"
	c := model.Client{Email: email, ID: "11111111-1111-1111-1111-111111111111", SubID: email, Enable: true, TotalGB: 10}
	ib := mkInbound(t, 52020, model.VLESS, clientsSettings(t, []model.Client{c}))
	if err := svc.SyncInbound(nil, ib.Id, []model.Client{c}); err != nil {
		t.Fatalf("seed linkage: %v", err)
	}
	mkTraffic(t, ib.Id, email, 0, 0, 10, 0, true)

	res, _, err := svc.BulkAdjust(inboundSvc, []string{email}, 0, -20, "", nil, "")
	if err != nil {
		t.Fatalf("BulkAdjust: %v", err)
	}
	if res.Adjusted != 0 {
		t.Fatalf("over-reduction should not adjust, got Adjusted=%d skipped=%v", res.Adjusted, res.Skipped)
	}
	if got := trafficOf(t, email).Total; got != 10 {
		t.Fatalf("quota reduced past zero was written as %d (0 means unlimited); want it left at 10", got)
	}
}

func TestBulkAdjust_FlowOnly_NoEnableChange(t *testing.T) {
	setupBulkDB(t)
	svc := &ClientService{}
	inboundSvc := &InboundService{}

	now := time.Now().UnixMilli()
	email := "flow@x"
	ib := seedLocalDisabledClient(t, svc, 52007, realityStream, email, 0, now-reenableDay, 0, 0)

	if _, _, err := svc.BulkAdjust(inboundSvc, []string{email}, 0, 0, "xtls-rprx-vision-udp443", nil, ""); err != nil {
		t.Fatalf("BulkAdjust: %v", err)
	}
	assertEnableEverywhere(t, svc, inboundSvc, ib.Id, email, false)
	if got := flowOf(t, svc, email); got != "xtls-rprx-vision-udp443" {
		t.Fatalf("%s: flow = %q, want xtls-rprx-vision-udp443", email, got)
	}
}

func TestBulkAdjust_UnlimitedExpiry_QuotaCleared_Reenables(t *testing.T) {
	setupBulkDB(t)
	svc := &ClientService{}
	inboundSvc := &InboundService{}

	email := "u@x"
	ib := seedLocalDisabledClient(t, svc, 52008, "", email, 100, 0, 100, 0)

	res, _, err := svc.BulkAdjust(inboundSvc, []string{email}, 0, 200, "", nil, "")
	if err != nil {
		t.Fatalf("BulkAdjust: %v", err)
	}
	if res.Adjusted != 1 {
		t.Fatalf("expected 1 adjusted, got %d (skipped=%v)", res.Adjusted, res.Skipped)
	}
	assertEnableEverywhere(t, svc, inboundSvc, ib.Id, email, true)
	if got := trafficOf(t, email).Total; got != 300 {
		t.Fatalf("%s: total = %d, want 300", email, got)
	}
}
