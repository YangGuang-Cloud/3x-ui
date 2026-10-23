package service

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/xray"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func bandwidthClient(t *testing.T) *ClientService {
	t.Helper()
	setupBulkDB(t)
	bin := t.TempDir()
	t.Setenv("XUI_BIN_FOLDER", bin)
	// The capability probe is tested against a harmless executable; application
	// callbacks below assert persistence and rollback without starting a proxy.
	name := xray.GetBinaryName()
	if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\necho 'Xray 26.10.10 SorvoRateV1'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	rec := model.ClientRecord{Email: "limited@test", Enable: true}
	if err := database.GetDB().Create(&rec).Error; err != nil {
		t.Fatal(err)
	}
	ib := mkInbound(t, 29171, model.VLESS, `{"clients":[]}`)
	if err := database.GetDB().Create(&model.ClientInbound{ClientId: rec.Id, InboundId: ib.Id}).Error; err != nil {
		t.Fatal(err)
	}
	return &ClientService{}
}

func TestClientBandwidthPersistenceAndRollback(t *testing.T) {
	svc := bandwidthClient(t)
	limits := model.ClientBandwidth{UploadMbps: 2, DownloadMbps: 8}
	if err := svc.SetBandwidth("limited@test", limits, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	raw, err := generatedClientBandwidth()
	if err != nil {
		t.Fatal(err)
	}
	var generated map[string]model.ClientBandwidth
	if err := json.Unmarshal(raw, &generated); err != nil {
		t.Fatal(err)
	}
	if generated["limited@test"].DownloadMbps != 8 {
		t.Fatalf("runtime limits: %s", raw)
	}
	calls := 0
	err = svc.SetBandwidth("limited@test", model.ClientBandwidth{UploadMbps: 0, DownloadMbps: 0}, func() error {
		calls++
		if calls == 1 {
			return errors.New("simulated start failure")
		}
		return nil
	})
	if err == nil || calls != 2 {
		t.Fatalf("rollback not attempted: %v, %d", err, calls)
	}
	restored, err := svc.GetBandwidth("limited@test")
	if err != nil || restored.DownloadMbps != 8 || restored.UploadMbps != 2 {
		t.Fatalf("limits not restored: %+v %v", restored, err)
	}
	if err := database.GetDB().Model(&model.ClientRecord{}).Where("id = ?", restored.ClientID).Update("email", "renamed@test").Error; err != nil {
		t.Fatal(err)
	}
	renamed, err := svc.GetBandwidth("renamed@test")
	if err != nil || renamed.DownloadMbps != 8 {
		t.Fatal("rename lost limits", err)
	}
}

func TestClientBandwidthValidationAndStockCore(t *testing.T) {
	svc := bandwidthClient(t)
	for _, v := range []float64{-1, .001, 1000001} {
		if err := svc.SetBandwidth("limited@test", model.ClientBandwidth{UploadMbps: v}, func() error { t.Fatal("invalid limit applied"); return nil }); err == nil {
			t.Fatal("accepted invalid limit", v)
		}
	}
	t.Setenv("XUI_BIN_FOLDER", t.TempDir())
	if err := svc.SetBandwidth("limited@test", model.ClientBandwidth{DownloadMbps: 1}, func() error { t.Fatal("stock core silently accepted limits"); return nil }); err == nil {
		t.Fatal("unsupported core was accepted")
	}
	value, err := svc.GetBandwidth("limited@test")
	if err != nil || value.DownloadMbps != 0 {
		t.Fatal("unsupported core changed stored state")
	}
}
