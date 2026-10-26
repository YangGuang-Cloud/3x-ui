package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// A prepaid plan must stop itself: once as many renewals have fired as the
// operator allowed, the client expires like any other (#5804).

// Catching up several missed periods spends one allowance per period: a client
// that was away for three cycles must not receive three of them for free.

// No cap set is the existing behaviour: renew for as long as the client keeps
// expiring.

// The cap has to survive the clients table, not just the settings JSON: an
// ordinary edit rebuilds the client from the record and writes it back (#5804).
func TestClientEditKeepsTheRenewalCap(t *testing.T) {
	setupBulkDB(t)
	svc := &InboundService{}
	db := database.GetDB()

	clients := []model.Client{
		{Email: "cap@x", ID: "44444444-4444-4444-4444-444444444444", Enable: true, Reset: 30, ResetMax: 3, ExpiryTime: time.Now().Add(24 * time.Hour).UnixMilli()},
	}
	ib := mkInbound(t, 30104, model.VLESS, clientsSettings(t, clients))
	if err := svc.clientService.SyncInbound(nil, ib.Id, clients); err != nil {
		t.Fatalf("SyncInbound: %v", err)
	}
	mkTraffic(t, ib.Id, "cap@x", 10, 20, 0, 0, true)

	rec, err := svc.clientService.GetRecordByEmail(nil, "cap@x")
	if err != nil {
		t.Fatalf("GetRecordByEmail: %v", err)
	}
	if rec.ResetMax != 3 {
		t.Fatalf("clients.reset_max = %d, want the 3 the client was created with", rec.ResetMax)
	}

	// What the edit dialog does: hydrate the record, change something else, save.
	edited := rec.ToClient()
	edited.Comment = "renamed"
	if _, err := svc.clientService.Update(svc, rec.Id, *edited, rec.LimitHwid); err != nil {
		t.Fatalf("Update: %v", err)
	}

	var stored model.Inbound
	if err := db.Where("id = ?", ib.Id).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Clients []model.Client `json:"clients"`
	}
	if err := json.Unmarshal([]byte(stored.Settings), &settings); err != nil {
		t.Fatalf("parse inbound settings: %v", err)
	}
	if len(settings.Clients) != 1 {
		t.Fatalf("inbound holds %d clients, want 1", len(settings.Clients))
	}
	if settings.Clients[0].ResetMax != 3 {
		t.Fatalf("inbound settings resetMax = %d after an unrelated edit, want 3: the cap was silently lifted", settings.Clients[0].ResetMax)
	}

	rec, err = svc.clientService.GetRecordByEmail(nil, "cap@x")
	if err != nil {
		t.Fatalf("GetRecordByEmail after edit: %v", err)
	}
	if rec.ResetMax != 3 {
		t.Fatalf("clients.reset_max = %d after an unrelated edit, want 3", rec.ResetMax)
	}
}

// A cap that runs out mid-catch-up leaves the client expired, so the renewal
// side effects must not fire: disableInvalidClients would undo them at once.

// The cap is useless if it can only be chosen once. The test above passes even
// without the record write, because nothing overwrites the value it checks.
func TestClientEditChangesTheRenewalCap(t *testing.T) {
	setupBulkDB(t)
	svc := &InboundService{}

	clients := []model.Client{
		{
			Email: "chg@x", ID: "77777777-7777-7777-7777-777777777777", Enable: true, Reset: 30, ResetMax: 3,
			ExpiryTime: time.Now().Add(24 * time.Hour).UnixMilli(),
		},
	}
	ib := mkInbound(t, 30106, model.VLESS, clientsSettings(t, clients))
	if err := svc.clientService.SyncInbound(nil, ib.Id, clients); err != nil {
		t.Fatalf("SyncInbound: %v", err)
	}
	mkTraffic(t, ib.Id, "chg@x", 0, 0, 0, 0, true)

	rec, err := svc.clientService.GetRecordByEmail(nil, "chg@x")
	if err != nil {
		t.Fatalf("GetRecordByEmail: %v", err)
	}

	// The customer buys another block of periods, which is the whole point of
	// the field being editable.
	edited := rec.ToClient()
	edited.ResetMax = 6
	if _, err := svc.clientService.Update(svc, rec.Id, *edited, rec.LimitHwid); err != nil {
		t.Fatalf("Update: %v", err)
	}

	rec, err = svc.clientService.GetRecordByEmail(nil, "chg@x")
	if err != nil {
		t.Fatalf("GetRecordByEmail after edit: %v", err)
	}
	if rec.ResetMax != 6 {
		t.Fatalf("clients.reset_max = %d after the operator raised the cap to 6", rec.ResetMax)
	}

	// Lifting the cap entirely has to work too.
	edited = rec.ToClient()
	edited.ResetMax = 0
	if _, err := svc.clientService.Update(svc, rec.Id, *edited, rec.LimitHwid); err != nil {
		t.Fatalf("Update to uncapped: %v", err)
	}
	rec, err = svc.clientService.GetRecordByEmail(nil, "chg@x")
	if err != nil {
		t.Fatalf("GetRecordByEmail after lifting the cap: %v", err)
	}
	if rec.ResetMax != 0 {
		t.Fatalf("clients.reset_max = %d after the operator lifted the cap", rec.ResetMax)
	}
}
