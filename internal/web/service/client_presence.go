package service

import (
	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"sync"
	"time"
)

type ClientPresence struct {
	ClientID      int   `json:"clientId"`
	OnlineSeconds int64 `json:"onlineSeconds"`
	OnlineIPs     int   `json:"onlineIPs"`
}

// Only contiguous successful observations are charged; process restarts and
// unavailable core statistics never turn downtime into online duration.
type PresenceTracker struct {
	mu       sync.Mutex
	last     int64
	veid     string
	previous map[int]int
	current  map[int]int
}

var clientPresence PresenceTracker

func RecordClientPresence(observed map[string]map[string]int64, available bool) error {
	return clientPresence.record(observed, available, time.Now().Unix())
}
func (p *PresenceTracker) record(observed map[string]map[string]int64, available bool, now int64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !available {
		p.last = 0
		p.current = nil
		p.previous = nil
		return nil
	}
	db := database.GetDB()
	var clients []model.ClientRecord
	if err := db.Select("id, email").Find(&clients).Error; err != nil {
		p.last = 0
		p.current = nil
		return err
	}
	current := map[int]int{}
	for _, c := range clients {
		current[c.Id] = len(observed[c.Email])
	}
	config, err := loadBandwagonConfig()
	if err != nil {
		p.last = 0
		p.current = nil
		return err
	}
	if config.Enabled && p.veid == config.VEID && p.last > 0 && now > p.last && now-p.last <= 30 {
		var cycle model.BandwagonCycle
		err = db.Where("ve_id = ? AND start <= ?", config.VEID, p.last).Order("next_reset DESC").Limit(1).Find(&cycle).Error
		if err == nil && cycle.ID > 0 {
			err = db.Transaction(func(tx *gorm.DB) error {
				for id, count := range current {
					if count == 0 || p.previous[id] == 0 {
						continue
					}
					for start := max(p.last, cycle.Start); start < now; {
						bucket := max(start/60*60, cycle.Start)
						end := min(now, (start/60+1)*60)
						if start < cycle.NextReset {
							end = min(end, cycle.NextReset)
						} else {
							bucket = max(bucket, cycle.NextReset)
						}
						row := model.ClientOnlineTime{VEID: config.VEID, ClientID: id, Bucket: bucket, Seconds: end - start}
						if e := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "ve_id"}, {Name: "client_id"}, {Name: "bucket"}}, DoUpdates: clause.Assignments(map[string]interface{}{"seconds": gorm.Expr("seconds + ?", row.Seconds)})}).Create(&row).Error; e != nil {
							return e
						}
						start = end
					}
				}
				return nil
			})
		}
	}
	p.last = now
	p.veid = config.VEID
	if !config.Enabled { p.veid = "" }
	p.previous = current
	p.current = current
	return err
}
func (p *PresenceTracker) snapshot(now int64) (map[int]int, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.last == 0 || now < p.last || now-p.last > 30 {
		return nil, false
	}
	out := map[int]int{}
	for id, n := range p.current {
		out[id] = n
	}
	return out, true
}
func populateClientPresence(out *BandwagonStatus, db *gorm.DB) error {
	counts, ok := clientPresence.snapshot(time.Now().Unix())
	out.PresenceAvailable = ok
	rows := []ClientPresence{}
	if out.Config.Enabled && out.Cycle != nil {
		if err := db.Model(&model.ClientOnlineTime{}).Select("client_id, SUM(seconds) AS online_seconds").Where("ve_id = ? AND bucket >= ? AND bucket < ?", out.Config.VEID, out.Cycle.Start, out.Cycle.NextReset).Group("client_id").Scan(&rows).Error; err != nil {
			return err
		}
	}
	seen := map[int]bool{}
	for i := range rows {
		rows[i].OnlineIPs = counts[rows[i].ClientID]
		seen[rows[i].ClientID] = true
	}
	for id, n := range counts {
		if !seen[id] {
			rows = append(rows, ClientPresence{ClientID: id, OnlineIPs: n})
		}
	}
	out.Presence = rows
	return nil
}
