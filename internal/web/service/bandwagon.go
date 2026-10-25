package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/config"
	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type BandwagonConfig struct {
	TrackingSince int64   `json:"trackingSince"`
	Enabled       bool    `json:"enabled"`
	VEID          string  `json:"veid"`
	APIKey        string  `json:"apiKey,omitempty"`
	UploadMbps    float64 `json:"uploadMbps"`
	DownloadMbps  float64 `json:"downloadMbps"`
}

var (
	bandwagonSyncMu          sync.Mutex
	bandwagonMu              sync.Mutex
	bandwagonLastAttempt     time.Time
	bandwagonLastError       string
	bandwagonAccountingError string
)

func bandwagonPath() string { return filepath.Join(config.GetDBFolderPath(), "bandwagon.json") }
func loadBandwagonConfig() (BandwagonConfig, error) {
	var c BandwagonConfig
	b, e := os.ReadFile(bandwagonPath())
	if os.IsNotExist(e) {
		return c, nil
	}
	if e != nil {
		return c, e
	}
	e = json.Unmarshal(b, &c)
	return c, e
}

func SaveBandwagonConfig(c BandwagonConfig) error {
	bandwagonMu.Lock()
	defer bandwagonMu.Unlock()
	old, e := loadBandwagonConfig()
	if e != nil {
		return errors.New("无法读取统计配置")
	}
	c.TrackingSince = old.TrackingSince
	if c.Enabled && (!old.Enabled || c.VEID != old.VEID || c.TrackingSince == 0) {
		c.TrackingSince = time.Now().Unix()
	}
	c.VEID = strings.TrimSpace(c.VEID)
	c.APIKey = strings.TrimSpace(c.APIKey)
	if c.APIKey == "" && c.VEID == old.VEID {
		c.APIKey = old.APIKey
	}
	if c.Enabled && (!regexp.MustCompile(`^[0-9]{1,20}$`).MatchString(c.VEID) || c.APIKey == "") {
		return errors.New("请填写有效 VEID 和 API Key")
	}
	for _, v := range []float64{c.UploadMbps, c.DownloadMbps} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1000000 {
			return errors.New("带宽必须为 0–1000000 Mbps，0 表示未知")
		}
	}
	if len(c.APIKey) > 512 {
		return errors.New("API Key 过长")
	}
	b, e := json.Marshal(c)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(config.GetDBFolderPath(), ".bandwagon-*")
	if e != nil {
		return errors.New("无法保存统计配置")
	}
	name := f.Name()
	defer os.Remove(name)
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e == nil {
		e = os.Rename(name, bandwagonPath())
	}
	if e != nil {
		return errors.New("无法保存统计配置")
	}
	if old.VEID != c.VEID || old.APIKey != c.APIKey {
		bandwagonLastAttempt = time.Time{}
		bandwagonLastError = ""
	}
	return nil
}

type bandwagonResponse struct {
	Error      *int     `json:"error"`
	Quota      *float64 `json:"plan_monthly_data"`
	Used       *float64 `json:"data_counter"`
	Multiplier *float64 `json:"monthly_data_multiplier"`
	NextReset  int64    `json:"data_next_reset"`
}

func fetchBandwagon(ctx context.Context, c BandwagonConfig, client *http.Client, endpoint string) (bandwagonResponse, error) {
	var r bandwagonResponse
	form := url.Values{"veid": {c.VEID}, "api_key": {c.APIKey}}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if e != nil {
		return r, errors.New("无法创建供应商请求")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "3x-ui-provider-monitor/1.0")
	resp, e := client.Do(req)
	if e != nil {
		return r, errors.New("搬瓦工连接失败，请稍后重试")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return r, fmt.Errorf("搬瓦工返回 HTTP %d", resp.StatusCode)
	}
	if e = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&r); e != nil {
		return r, errors.New("搬瓦工返回无效数据")
	}
	if r.Error == nil || *r.Error != 0 {
		return r, errors.New("搬瓦工拒绝查询，请检查 VEID / API Key 或稍后重试")
	}
	if r.Quota == nil || r.Used == nil || r.Multiplier == nil || *r.Quota <= 0 || *r.Used < 0 || *r.Multiplier <= 0 || r.NextReset <= 0 {
		return r, errors.New("搬瓦工流量字段不完整")
	}
	for _, v := range []float64{*r.Quota, *r.Used, *r.Multiplier, *r.Quota * *r.Multiplier, *r.Used * *r.Multiplier} {
		if math.IsInf(v, 0) || math.IsNaN(v) || v > 9e18 {
			return r, errors.New("搬瓦工流量字段超出范围")
		}
	}
	return r, nil
}

// No write/control endpoint is exposed. The secret never appears in a URL or error.
func SyncBandwagon() error {
	bandwagonSyncMu.Lock()
	defer bandwagonSyncMu.Unlock()
	bandwagonMu.Lock()
	c, e := loadBandwagonConfig()
	if e != nil {
		bandwagonMu.Unlock()
		return errors.New("无法读取统计配置")
	}
	if !c.Enabled {
		bandwagonMu.Unlock()
		return nil
	}
	if time.Since(bandwagonLastAttempt) < 5*time.Minute {
		message := bandwagonLastError
		bandwagonMu.Unlock()
		if message != "" {
			return errors.New(message)
		}
		return nil
	}
	bandwagonLastAttempt = time.Now()
	bandwagonMu.Unlock()
	// No config/status/accounting lock is held during the provider request.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	r, e := fetchBandwagon(ctx, c, client, "https://api.64clouds.com/v1/getServiceInfo")
	bandwagonMu.Lock()
	defer bandwagonMu.Unlock()
	current, readErr := loadBandwagonConfig()
	if readErr != nil {
		return errors.New("无法读取统计配置")
	}
	if !current.Enabled || current.VEID != c.VEID || current.APIKey != c.APIKey {
		return nil
	}
	if e == nil {
		e = applyBandwagonSnapshot(database.GetDB(), c.VEID, r, time.Now().Unix())
		if e == nil && current.TrackingSince > 0 {
			e = database.GetDB().Model(&model.BandwagonCycle{}).Where("ve_id = ? AND next_reset = ? AND start < ?", c.VEID, r.NextReset, current.TrackingSince).Update("partial", true).Error
		}
	}
	if e != nil {
		bandwagonLastError = e.Error()
		return e
	}
	bandwagonLastError = ""
	return nil
}

func applyBandwagonSnapshot(db *gorm.DB, veid string, r bandwagonResponse, now int64) error {
	if r.NextReset <= now {
		return errors.New("等待搬瓦工更新下个账期，暂不清零")
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var previous model.BandwagonCycle
		e := tx.Where("ve_id = ?", veid).Order("next_reset DESC").First(&previous).Error
		if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		if e == nil && r.NextReset < previous.NextReset {
			return errors.New("供应商账期倒退，保留现有统计")
		}
		cycle := previous
		if previous.ID == 0 || r.NextReset != previous.NextReset {
			cycle = model.BandwagonCycle{VEID: veid, NextReset: r.NextReset, Start: now, Partial: true}
			if previous.ID > 0 {
				if now < previous.NextReset {
					return errors.New("重置时间提前变更，保留现有账期等待确认")
				}
				if r.NextReset-previous.NextReset <= 40*86400 {
					cycle.Start = previous.NextReset
					cycle.Partial = false
				}
			}
		}
		cycle.Quota = *r.Quota * *r.Multiplier
		cycle.Used = *r.Used * *r.Multiplier
		cycle.Multiplier = *r.Multiplier
		cycle.UpdatedAtUnix = now
		return tx.Save(&cycle).Error
	})
}

// Runs after primary accounting commits. Provider/network failures can never
// roll back client quota counters, restart Xray or disable clients.
func recordBandwagonTraffic(traffics []*xray.ClientTraffic) {
	if len(traffics) == 0 {
		return
	}
	c, e := loadBandwagonConfig()
	if e != nil || !c.Enabled {
		return
	}
	e = database.GetDB().Transaction(func(tx *gorm.DB) error {
		canonical, e := canonicalizeClientTraffic(tx, traffics)
		if e != nil {
			return e
		}
		minute := time.Now().Unix() / 60 * 60
		for _, t := range canonical {
			up, down := max(t.Up, 0), max(t.Down, 0)
			if up == 0 && down == 0 {
				continue
			}
			var client model.ClientRecord
			e = tx.Select("id").Where("email = ?", t.Email).Take(&client).Error
			if errors.Is(e, gorm.ErrRecordNotFound) {
				continue
			}
			if e != nil {
				return e
			}
			row := model.BandwagonTraffic{VEID: c.VEID, ClientID: client.Id, Minute: minute, Up: up, Down: down}
			if e = tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "ve_id"}, {Name: "client_id"}, {Name: "minute"}}, DoUpdates: clause.Assignments(map[string]any{"up": gorm.Expr("bandwagon_traffics.up + ?", up), "down": gorm.Expr("bandwagon_traffics.down + ?", down)})}).Create(&row).Error; e != nil {
				return e
			}
		}
		return nil
	})
	if e != nil {
		logger.Warning("Provider usage ledger write failed; primary quota accounting was preserved")
		bandwagonMu.Lock()
		bandwagonAccountingError = "本地周期统计曾写入失败，统计可能不完整"
		bandwagonMu.Unlock()
	}
}

type (
	BandwagonUsage struct {
		ClientID int   `json:"clientId"`
		Up       int64 `json:"up"`
		Down     int64 `json:"down"`
	}
	BandwagonStatus struct {
		Config        BandwagonConfig        `json:"config"`
		HasKey        bool                   `json:"hasKey"`
		Cycle         *model.BandwagonCycle  `json:"cycle"`
		History       []model.BandwagonCycle `json:"history"`
		Clients       []BandwagonUsage       `json:"clients"`
		Total         int64                  `json:"total"`
		Stale         bool                   `json:"stale"`
		AwaitingReset bool                   `json:"awaitingReset"`
		Error         string                 `json:"error"`
	}
)

func GetBandwagonStatus() (BandwagonStatus, error) {
	bandwagonMu.Lock()
	c, e := loadBandwagonConfig()
	lastError := bandwagonLastError
	gap := bandwagonAccountingError
	bandwagonMu.Unlock()
	out := BandwagonStatus{Config: c, HasKey: c.APIKey != "", Clients: []BandwagonUsage{}, History: []model.BandwagonCycle{}, Error: strings.TrimSpace(lastError + " " + gap)}
	out.Config.APIKey = ""
	if e != nil {
		return out, errors.New("无法读取统计配置")
	}
	if !c.Enabled {
		return out, nil
	}
	db := database.GetDB()
	if e = db.Where("ve_id = ?", c.VEID).Order("next_reset DESC").Limit(13).Find(&out.History).Error; e != nil {
		return out, e
	}
	if len(out.History) == 0 {
		return out, nil
	}
	out.Cycle = &out.History[0]
	out.Stale = time.Now().Unix()-out.Cycle.UpdatedAtUnix > 600
	out.AwaitingReset = time.Now().Unix() >= out.Cycle.NextReset
	e = db.Model(&model.BandwagonTraffic{}).Select("client_id, SUM(up) AS up, SUM(down) AS down").Where("ve_id = ? AND minute >= ? AND minute < ?", c.VEID, out.Cycle.Start/60*60, out.Cycle.NextReset/60*60).Group("client_id").Scan(&out.Clients).Error
	for _, v := range out.Clients {
		out.Total += v.Up + v.Down
	}
	return out, e
}
