package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

// pinPanelZone fixes the panel time zone so the assertions below can talk about
// calendar days without the test machine's own zone shifting them.
func pinPanelZone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("zone database unavailable: %v", err)
	}
	if err := database.GetDB().Create(&model.Setting{Key: "timeLocation", Value: name}).Error; err != nil {
		t.Fatalf("pin panel zone: %v", err)
	}
	return loc
}

// Calendar mode renews on the same day each month. The interval mode drifts —
// 30 days from 31 January is 2 March — which is the whole reason for the mode.

// A client billed on the 31st keeps that day, borrowing the last day only in
// months that are too short for it.

// Deliberately not built on nextCalendarRenewal: it walks a day at a time and
// derives month length from time.Date's own zero-day trick, so it can disagree.

// Interval clients must be untouched by the new field.

// The selection filter is what keeps a row with neither mode configured out of
// the renewal loop. That matters more than it looks: the interval step is
// reset*24h, so a zero interval reaching that loop would spin forever on the
// single traffic writer. The guard in the loop is a second line of defence and
// is deliberately unreachable while this filter holds.

// The billing day has to survive the clients table, not just the settings JSON:
// an ordinary edit rebuilds the client from the record and writes it back (#6106).
func TestClientEditKeepsTheBillingDay(t *testing.T) {
	setupBulkDB(t)
	svc := &InboundService{}
	db := database.GetDB()

	clients := []model.Client{
		{Email: "keep@x", ID: "55555555-5555-5555-5555-555555555555", Enable: true, ResetDay: 20, ExpiryTime: time.Now().Add(24 * time.Hour).UnixMilli()},
	}
	ib := mkInbound(t, 30205, model.VLESS, clientsSettings(t, clients))
	if err := svc.clientService.SyncInbound(nil, ib.Id, clients); err != nil {
		t.Fatalf("SyncInbound: %v", err)
	}
	mkTraffic(t, ib.Id, "keep@x", 10, 20, 0, 0, true)

	rec, err := svc.clientService.GetRecordByEmail(nil, "keep@x")
	if err != nil {
		t.Fatalf("GetRecordByEmail: %v", err)
	}
	if rec.ResetDay != 20 {
		t.Fatalf("clients.reset_day = %d, want the 20 the client was created with", rec.ResetDay)
	}

	// What the edit dialog does: hydrate the record, change something else, save.
	edited := rec.ToClient()
	edited.Comment = "renamed"
	if _, err := svc.clientService.Update(svc, rec.Id, *edited, rec.LimitHwid); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// The inbound JSON is what xray and the edit dialog read back, and it is
	// rebuilt from the record, so it is where a dropped converter field shows.
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
	if settings.Clients[0].ResetDay != 20 {
		t.Fatalf("inbound settings resetDay = %d after an unrelated edit, want 20: calendar mode was silently turned off", settings.Clients[0].ResetDay)
	}

	rec, err = svc.clientService.GetRecordByEmail(nil, "keep@x")
	if err != nil {
		t.Fatalf("GetRecordByEmail after edit: %v", err)
	}
	if rec.ResetDay != 20 {
		t.Fatalf("clients.reset_day = %d after an unrelated edit, want 20", rec.ResetDay)
	}

	var row xray.ClientTraffic
	if err := db.Where("email = ?", "keep@x").First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.ResetDay != 20 {
		t.Fatalf("client_traffics.reset_day = %d after an unrelated edit, want 20", row.ResetDay)
	}
}

// The billing day is useless if it can only be chosen once. The test above
// passes even without the record write, because nothing overwrites the value
// it checks; this one fails without it.
func TestClientEditChangesTheBillingDay(t *testing.T) {
	setupBulkDB(t)
	svc := &InboundService{}

	clients := []model.Client{
		{
			Email: "chg@x", ID: "77777777-7777-7777-7777-777777777777", Enable: true, ResetDay: 20,
			ExpiryTime: time.Now().Add(24 * time.Hour).UnixMilli(),
		},
	}
	ib := mkInbound(t, 30206, model.VLESS, clientsSettings(t, clients))
	if err := svc.clientService.SyncInbound(nil, ib.Id, clients); err != nil {
		t.Fatalf("SyncInbound: %v", err)
	}
	mkTraffic(t, ib.Id, "chg@x", 0, 0, 0, 0, true)

	rec, err := svc.clientService.GetRecordByEmail(nil, "chg@x")
	if err != nil {
		t.Fatalf("GetRecordByEmail: %v", err)
	}
	edited := rec.ToClient()
	edited.ResetDay = 5
	if _, err := svc.clientService.Update(svc, rec.Id, *edited, rec.LimitHwid); err != nil {
		t.Fatalf("Update: %v", err)
	}

	rec, err = svc.clientService.GetRecordByEmail(nil, "chg@x")
	if err != nil {
		t.Fatalf("GetRecordByEmail after edit: %v", err)
	}
	if rec.ResetDay != 5 {
		t.Fatalf("clients.reset_day = %d after the operator moved the billing day to the 5th", rec.ResetDay)
	}

	// Turning calendar mode off has to work too.
	edited = rec.ToClient()
	edited.ResetDay = 0
	edited.Reset = 30
	if _, err := svc.clientService.Update(svc, rec.Id, *edited, rec.LimitHwid); err != nil {
		t.Fatalf("Update back to interval mode: %v", err)
	}
	rec, err = svc.clientService.GetRecordByEmail(nil, "chg@x")
	if err != nil {
		t.Fatalf("GetRecordByEmail after switching mode: %v", err)
	}
	if rec.ResetDay != 0 {
		t.Fatalf("clients.reset_day = %d after the operator switched back to interval mode", rec.ResetDay)
	}
}

// The two renewal features meet here: a calendar client is capped like an
// interval one, spending one allowance per month rather than per tick.

// Inclusive EOM 23:59:59 must not spend an allowance on the one-second snap
// to the billing midnight, or resetMax=1 leaves the client expired (#6300).

// Unlimited EOM 23:59:59 must charge one renewal for the month, not two.

// Mid-day expiry on the day before billing still charges for alignment; only
// the inclusive 23:59:59 instant is a free snap (#6300).
