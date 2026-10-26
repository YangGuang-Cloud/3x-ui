package job

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

func initResetJobDB(t *testing.T) {
	t.Helper()
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	dbtest.InitDB(t, filepath.Join(dbDir, "x-ui.db"))
}

type seededClient struct {
	email        string
	cycle        string
	day          int
	recordEnable bool
	quotaEnable  bool
	total        int64
}

// seedClientOnCycle creates an inbound that never resets on its own, a client
// carrying its own cycle, and the client_inbounds link the reset path resolves
// through — without it every reset falls into the orphaned-client branch.
func seedClientOnCycle(t *testing.T, port int, c seededClient) {
	t.Helper()
	db := database.GetDB()

	client := model.Client{
		Email: c.email, ID: uuidFor(port), Enable: c.recordEnable,
		TrafficReset: c.cycle, TrafficResetDay: c.day,
	}
	settings, err := json.Marshal(map[string]any{"clients": []model.Client{client}})
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	ib := model.Inbound{
		UserId: 1, Enable: true, Port: port, Protocol: model.VLESS,
		Tag: "inbound-" + c.email, TrafficReset: "never", Settings: string(settings),
	}
	if err := db.Create(&ib).Error; err != nil {
		t.Fatalf("create inbound: %v", err)
	}
	rec := model.ClientRecord{
		Email: c.email, UUID: client.ID, Enable: c.recordEnable,
		TrafficReset: c.cycle, TrafficResetDay: c.day,
	}
	if err := db.Create(&rec).Error; err != nil {
		t.Fatalf("create client record: %v", err)
	}
	// gorm skips a false bool on insert, so the column default:true wins; the
	// disabled case has to be written back explicitly.
	if err := db.Model(&model.ClientRecord{}).Where("id = ?", rec.Id).
		Update("enable", c.recordEnable).Error; err != nil {
		t.Fatalf("set record enable: %v", err)
	}
	if err := db.Create(&model.ClientInbound{ClientId: rec.Id, InboundId: ib.Id}).Error; err != nil {
		t.Fatalf("link client to inbound: %v", err)
	}
	if err := db.Create(&xray.ClientTraffic{
		InboundId: ib.Id, Email: c.email, Enable: c.quotaEnable, Up: 500, Down: 700, Total: c.total,
	}).Error; err != nil {
		t.Fatalf("create traffic: %v", err)
	}
}

func uuidFor(port int) string {
	return "00000000-0000-0000-0000-0000000" + string(rune('0'+port/10000%10)) +
		string(rune('0'+port/1000%10)) + string(rune('0'+port/100%10)) +
		string(rune('0'+port/10%10)) + string(rune('0'+port%10))
}

func trafficFor(t *testing.T, email string) xray.ClientTraffic {
	t.Helper()
	var row xray.ClientTraffic
	if err := database.GetDB().Where("email = ?", email).First(&row).Error; err != nil {
		t.Fatalf("read traffic for %s: %v", email, err)
	}
	return row
}

func recordFor(t *testing.T, email string) model.ClientRecord {
	t.Helper()
	var rec model.ClientRecord
	if err := database.GetDB().Where("email = ?", email).First(&rec).Error; err != nil {
		t.Fatalf("read record for %s: %v", email, err)
	}
	return rec
}

func TestPeriodicTrafficResetClients(t *testing.T) {

	for _, period := range []Period{"hourly", "daily", "weekly", "monthly"} {
		t.Run(string(period), func(t *testing.T) {
			initResetJobDB(t)
			seedClientOnCycle(t, 54321, seededClient{email: "retired@test", cycle: string(period), day: time.Now().Day(), recordEnable: true, quotaEnable: false, total: 100})
			NewPeriodicTrafficResetJob(period, time.UTC).Run()
			row := trafficFor(t, "retired@test")
			if row.Up != 500 || row.Down != 700 || row.Enable {
				t.Fatalf("retired schedule changed usage/gate: %+v", row)
			}
		})
	}

}

func settingsEnableOf(t *testing.T, port int) bool {
	t.Helper()
	var stored model.Inbound
	if err := database.GetDB().Where("port = ?", port).First(&stored).Error; err != nil {
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
	return settings.Clients[0].Enable
}
