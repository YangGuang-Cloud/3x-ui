package service

import (
	"slices"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ClientPresence struct {
	ClientID      int      `json:"clientId"`
	OnlineSeconds int64    `json:"onlineSeconds"`
	OnlineIPs     int      `json:"onlineIPs"`
	OnlineIPList  []string `json:"onlineIPList,omitempty" gorm:"-"`
}

// Only contiguous successful observations are charged; process restarts and
// unavailable core statistics never turn downtime into online duration.
type PresenceTracker struct {
	mu       sync.Mutex
	last     int64
	veid     string
	previous map[int]bool
	current  map[int][]string
}

var clientPresence PresenceTracker

// RecordClientPresence uses the same active accounts as the panel's online
// indicator. Traffic can show activity even when Xray has no source IP to report.
func RecordClientPresence(activeEmails []string, users []xray.OnlineUser, ipsAvailable bool) error {
	var ipsByEmail map[string][]string
	if ipsAvailable {
		ipsByEmail = make(map[string][]string, len(users))
		for _, user := range users {
			for _, ip := range user.IPs {
				ipsByEmail[user.Email] = append(ipsByEmail[user.Email], ip.IP)
			}
		}
		for email, ips := range ipsByEmail {
			slices.Sort(ips)
			ipsByEmail[email] = slices.Compact(ips)
		}
	}
	return clientPresence.record(activeEmails, ipsByEmail, time.Now().Unix())
}

func (p *PresenceTracker) record(activeEmails []string, ipsByEmail map[string][]string, now int64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if activeEmails == nil && ipsByEmail == nil {
		p.last = 0
		p.current = nil
		p.previous = nil
		return nil
	}
	config, err := loadBandwagonConfig()
	if err != nil {
		p.last = 0
		p.current = nil
		return err
	}
	db := database.GetDB()
	var clients []model.ClientRecord
	if err := db.Select("id, email").Find(&clients).Error; err != nil {
		p.last = 0
		p.current = nil
		return err
	}
	active := make(map[string]bool, len(activeEmails))
	for _, email := range activeEmails {
		active[email] = true
	}
	var currentIPs map[int][]string
	if ipsByEmail != nil {
		currentIPs = make(map[int][]string, len(ipsByEmail))
	}
	current := make(map[int]bool, len(active))
	for _, c := range clients {
		if active[c.Email] {
			current[c.Id] = true
		}
		if ips := ipsByEmail[c.Email]; len(ips) > 0 {
			currentIPs[c.Id] = ips
		}
	}
	if config.Enabled && p.veid == config.VEID && p.last > 0 && now > p.last && now-p.last <= 30 {
		var cycle model.BandwagonCycle
		err = db.Where("ve_id = ? AND start <= ?", config.VEID, p.last).Order("next_reset DESC").Limit(1).Find(&cycle).Error
		if err == nil && cycle.ID > 0 {
			err = db.Transaction(func(tx *gorm.DB) error {
				for id := range current {
					if !p.previous[id] {
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
	if !config.Enabled {
		p.veid = ""
	}
	p.previous = current
	p.current = currentIPs
	return err
}

func (p *PresenceTracker) snapshot(now int64) (map[int][]string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.current == nil || p.last == 0 || now < p.last || now-p.last > 30 {
		return nil, false
	}
	out := make(map[int][]string, len(p.current))
	for id, ips := range p.current {
		out[id] = slices.Clone(ips)
	}
	return out, true
}

func populateClientPresence(out *BandwagonStatus, db *gorm.DB) error {
	onlineIPs, ok := clientPresence.snapshot(time.Now().Unix())
	out.PresenceAvailable = ok
	rows := []ClientPresence{}
	if out.Config.Enabled && out.Cycle != nil {
		if err := db.Model(&model.ClientOnlineTime{}).Select("client_id, SUM(seconds) AS online_seconds").Where("ve_id = ? AND bucket >= ? AND bucket < ?", out.Config.VEID, out.Cycle.Start, out.Cycle.NextReset).Group("client_id").Scan(&rows).Error; err != nil {
			return err
		}
	}
	seen := map[int]bool{}
	for i := range rows {
		rows[i].OnlineIPList = onlineIPs[rows[i].ClientID]
		rows[i].OnlineIPs = len(rows[i].OnlineIPList)
		seen[rows[i].ClientID] = true
	}
	for id, ips := range onlineIPs {
		if !seen[id] {
			rows = append(rows, ClientPresence{ClientID: id, OnlineIPs: len(ips), OnlineIPList: ips})
		}
	}
	out.Presence = rows
	return nil
}
