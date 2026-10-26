package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

func bwgResponse(next int64, used float64) bandwagonResponse {
	zero := 0
	quota, mult := float64(10000), float64(2)
	return bandwagonResponse{Error: &zero, Quota: &quota, Used: &used, Multiplier: &mult, NextReset: next}
}

func TestBandwagonCycleRolloverIdempotency(t *testing.T) {
	setupBulkDB(t)
	db := database.GetDB()
	now := time.Now().Unix() / 60 * 60
	if e := SaveBandwagonConfig(BandwagonConfig{Enabled: true, VEID: "123", APIKey: "private_test"}); e != nil {
		t.Fatal(e)
	}
	firstEnd := now + 3600
	if e := applyBandwagonSnapshot(db, "123", bwgResponse(firstEnd, 100), now); e != nil {
		t.Fatal(e)
	}
	db.Create(&model.BandwagonTraffic{VEID: "123", ClientID: 1, Minute: now, Up: 120, Down: 80})
	db.Create(&model.BandwagonTraffic{VEID: "123", ClientID: 1, Minute: firstEnd, Up: 30, Down: 20})
	status, e := GetBandwagonStatus()
	if e != nil || status.Total != 200 || !status.Cycle.Partial || status.Cycle.Quota != 20000 || status.Cycle.Used != 200 {
		t.Fatalf("first cycle: %+v %v", status, e)
	}
	next := firstEnd + 30*86400
	if e := applyBandwagonSnapshot(db, "123", bwgResponse(next, 0), firstEnd-1); e == nil {
		t.Fatal("premature rollover accepted")
	}
	if e := applyBandwagonSnapshot(db, "123", bwgResponse(firstEnd, 0), firstEnd+1); e == nil {
		t.Fatal("stale reset should wait")
	}
	for range 2 {
		if e := applyBandwagonSnapshot(db, "123", bwgResponse(next, 10), firstEnd+1); e != nil {
			t.Fatal(e)
		}
	}
	status, e = GetBandwagonStatus()
	if e != nil || status.Total != 50 || status.Cycle.Partial || len(status.History) != 2 {
		t.Fatalf("rollover: %+v %v", status, e)
	}
	if e := applyBandwagonSnapshot(db, "123", bwgResponse(firstEnd, 10), now); e == nil {
		t.Fatal("backward reset accepted")
	}
	var n int64
	db.Model(&model.BandwagonTraffic{}).Count(&n)
	if n != 2 {
		t.Fatal("history deleted")
	}
}

func TestBandwagonLedgerPreservesQuotaAndIdentity(t *testing.T) {
	setupBulkDB(t)
	db := database.GetDB()
	SaveBandwagonConfig(BandwagonConfig{Enabled: true, VEID: "123", APIKey: "secret"})
	rec := model.ClientRecord{Email: "local@test", Enable: true}
	if e := db.Create(&rec).Error; e != nil {
		t.Fatal(e)
	}
	db.Create(&xray.ClientTraffic{Email: rec.Email, Up: 100, Down: 200})
	recordBandwagonTraffic([]*xray.ClientTraffic{{Email: rec.Email, Up: 5, Down: 10}, {Email: rec.Email, Up: 7, Down: 8}})
	db.Model(&model.ClientRecord{}).Where("id = ?", rec.Id).Update("email", "renamed@test")
	recordBandwagonTraffic([]*xray.ClientTraffic{{Email: "renamed@test", Up: 3, Down: 2}})
	var row model.BandwagonTraffic
	if e := db.First(&row).Error; e != nil || row.Up != 15 || row.Down != 20 || row.ClientID != rec.Id {
		t.Fatalf("ledger: %+v %v", row, e)
	}
	var quota xray.ClientTraffic
	db.First(&quota)
	if quota.Up != 100 || quota.Down != 200 {
		t.Fatal("quota modified")
	}
	db.Model(&xray.ClientTraffic{}).Where("id = ?", quota.Id).Updates(map[string]any{"up": 0, "down": 0})
	db.First(&row)
	if row.Up != 15 || row.Down != 20 {
		t.Fatal("quota reset cleared provider ledger")
	}
	b, e := json.Marshal(mustBandwagonStatus(t))
	if e != nil || strings.Contains(string(b), "secret") {
		t.Fatal("secret leaked in status")
	}
	st, e := os.Stat(filepath.Join(os.Getenv("XUI_DB_FOLDER"), "bandwagon.json"))
	if e != nil || st.Mode().Perm() != 0o600 {
		t.Fatal("secret file permissions")
	}
}

func mustBandwagonStatus(t *testing.T) BandwagonStatus {
	t.Helper()
	s, e := GetBandwagonStatus()
	if e != nil {
		t.Fatal(e)
	}
	return s
}

func TestBandwagonReadOnlyHTTPAndValidation(t *testing.T) {
	response := `{"error":0,"plan_monthly_data":100,"data_counter":10,"monthly_data_multiplier":0.5,"data_next_reset":2000000000}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.RawQuery != "" {
			t.Error("credentials must be POST body")
		}
		r.ParseForm()
		if r.Form.Get("api_key") != "private_secret" {
			t.Error("missing key")
		}
		w.Write([]byte(response))
	}))
	defer server.Close()
	cfg := BandwagonConfig{VEID: "1", APIKey: "private_secret"}
	r, e := fetchBandwagon(context.Background(), cfg, server.Client(), server.URL)
	if e != nil || *r.Multiplier != 0.5 {
		t.Fatal(r, e)
	}
	for _, bad := range []string{`{}`, `{"error":1,"message":"private_secret"}`, `{"error":0,"plan_monthly_data":100,"data_counter":10,"monthly_data_multiplier":0,"data_next_reset":2000000000}`} {
		response = bad
		_, e = fetchBandwagon(context.Background(), cfg, server.Client(), server.URL)
		if e == nil || strings.Contains(e.Error(), "private_secret") {
			t.Fatal("invalid response or secret handling", e)
		}
	}
}

func TestBandwagonSecretPreservationAndChange(t *testing.T) {
	setupBulkDB(t)
	cfg := BandwagonConfig{Enabled: true, VEID: "1", APIKey: "private_secret"}
	if e := SaveBandwagonConfig(cfg); e != nil {
		t.Fatal(e)
	}
	cfg.APIKey = ""
	cfg.DownloadMbps = 1000
	if e := SaveBandwagonConfig(cfg); e != nil {
		t.Fatal(e)
	}
	loaded, _ := loadBandwagonConfig()
	if loaded.APIKey != "private_secret" {
		t.Fatal("blank key erased credential")
	}
	cfg.VEID = "2"
	if e := SaveBandwagonConfig(cfg); e == nil {
		t.Fatal("different VPS reused old key")
	}
}

func TestBandwagonSlimCarriesStableIdentity(t *testing.T) {
	row := toClientSlim(ClientWithAttachments{ClientRecord: model.ClientRecord{Id: 42, Email: "a"}})
	if row.ID != 42 {
		t.Fatal("paged client dropped identity required for cycle accounting")
	}
}

func TestBandwagonLedgerFailureDoesNotLosePrimaryTraffic(t *testing.T) {
	setupBulkDB(t)
	db := database.GetDB()
	SaveBandwagonConfig(BandwagonConfig{Enabled: true, VEID: "1", APIKey: "secret"})
	defer func() { bandwagonMu.Lock(); bandwagonAccountingError = ""; bandwagonMu.Unlock() }()
	rec := model.ClientRecord{Email: "a", Enable: true}
	db.Create(&rec)
	db.Create(&xray.ClientTraffic{Email: "a", Enable: true})
	db.Migrator().DropTable(&model.BandwagonTraffic{})
	_, _, err := (&InboundService{}).AddTraffic(nil, []*xray.ClientTraffic{{Email: "a", Up: 500, Down: 700}})
	if err != nil {
		t.Fatal("optional ledger blocked primary accounting", err)
	}
	var row xray.ClientTraffic
	db.Where("email = ?", "a").First(&row)
	if row.Up != 500 || row.Down != 700 {
		t.Fatal("primary traffic was lost", row)
	}
}
