package service

import (
	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"testing"
)

func TestPresenceCycleAndInterruptions(t *testing.T) {
	setupBulkDB(t)
	db := database.GetDB()
	if err := db.AutoMigrate(&model.ClientOnlineTime{}); err != nil {
		t.Fatal(err)
	}
	if err := SaveBandwagonConfig(BandwagonConfig{Enabled: true, VEID: "123", APIKey: "test"}); err != nil {
		t.Fatal(err)
	}
	c := model.ClientRecord{Email: "presence@test", Enable: true}
	if err := db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	const start int64 = 1800000000
	const reset = start + 25
	if err := applyBandwagonSnapshot(db, "123", bwgResponse(reset, 0), start); err != nil {
		t.Fatal(err)
	}
	p := PresenceTracker{}
	online := map[string]map[string]int64{c.Email: {"1.1.1.1": start, "2.2.2.2": start}}
	run := func(at int64, obs map[string]map[string]int64, ok bool) {
		t.Helper()
		if err := p.record(obs, ok, at); err != nil {
			t.Fatal(err)
		}
	}
	sum := func(lo, hi int64) int64 {
		var n int64
		db.Model(&model.ClientOnlineTime{}).Select("COALESCE(SUM(seconds),0)").Where("bucket >= ? AND bucket < ?", lo, hi).Scan(&n)
		return n
	}
	run(start, online, true)
	run(start+10, online, true)
	run(start+20, online, true)
	run(start+30, online, true)
	if n := sum(start, reset); n != 25 {
		t.Fatalf("old cycle=%d", n)
	}
	if n := sum(reset, reset+100); n != 5 {
		t.Fatalf("pending new cycle=%d", n)
	}
	if counts, ok := p.snapshot(start + 30); !ok || counts[c.Id] != 2 {
		t.Fatal("IP count must be 2")
	}
	if err := applyBandwagonSnapshot(db, "123", bwgResponse(reset+1000, 0), start+31); err != nil {
		t.Fatal(err)
	}
	run(start+40, nil, true)
	run(start+50, online, true)
	run(start+60, online, true)
	if n := sum(reset, reset+1000); n != 15 {
		t.Fatalf("offline pause=%d", n)
	}
	run(start+70, nil, false)
	run(start+80, online, true)
	run(start+90, online, true)
	run(start+200, online, true)
	if n := sum(reset, reset+1000); n != 25 {
		t.Fatalf("gap charged=%d", n)
	}
	p = PresenceTracker{}
	run(start+210, online, true)
	run(start+220, online, true)
	if n := sum(reset, reset+1000); n != 35 {
		t.Fatalf("restart lost or added time=%d", n)
	}
	if _, ok := p.snapshot(start + 251); ok {
		t.Fatal("stale counts exposed")
	}
	// Rename keeps the same account ledger; two IPs never double the duration.
	db.Model(&c).Update("email", "renamed@test")
	run(start+230, map[string]map[string]int64{"renamed@test": {"1.1.1.1": start}}, true)
	if n := sum(reset, reset+1000); n != 45 {
		t.Fatalf("rename=%d", n)
	}
}

func TestPresenceConfirmedResetBetweenSamples(t *testing.T) {
	setupBulkDB(t)
	db := database.GetDB()
	if err := SaveBandwagonConfig(BandwagonConfig{Enabled: true, VEID: "123", APIKey: "test"}); err != nil {
		t.Fatal(err)
	}
	c := model.ClientRecord{Email: "boundary@test", Enable: true}
	db.Create(&c)
	const start int64 = 1800000000
	if err := applyBandwagonSnapshot(db, "123", bwgResponse(start+5, 0), start); err != nil {
		t.Fatal(err)
	}
	p := PresenceTracker{}
	observed := map[string]map[string]int64{c.Email: {"1.1.1.1": start}}
	if err := p.record(observed, true, start); err != nil {
		t.Fatal(err)
	}
	if err := applyBandwagonSnapshot(db, "123", bwgResponse(start+1000, 0), start+6); err != nil {
		t.Fatal(err)
	}
	if err := p.record(observed, true, start+10); err != nil {
		t.Fatal(err)
	}
	var rows []model.ClientOnlineTime
	db.Order("bucket").Find(&rows)
	if len(rows) != 2 || rows[0].Seconds != 5 || rows[1].Seconds != 5 {
		t.Fatalf("boundary: %+v", rows)
	}
	status, err := GetBandwagonStatus()
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Presence) != 1 || status.Presence[0].OnlineSeconds != 5 {
		t.Fatalf("new cycle status: %+v", status.Presence)
	}
}
