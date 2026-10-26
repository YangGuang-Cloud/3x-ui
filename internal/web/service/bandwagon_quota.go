package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

// Cycle quotas cover local-only clients. Remote panels do not report into this
// host's minute ledger and must retain their existing cross-panel quota policy.
func bandwagonLocalClientSQL(id string) string {
	return "EXISTS (SELECT 1 FROM client_inbounds bci JOIN inbounds bi ON bi.id = bci.inbound_id WHERE bci.client_id = " + id + " AND bi.node_id IS NULL) AND NOT EXISTS (SELECT 1 FROM client_inbounds bci JOIN inbounds bi ON bi.id = bci.inbound_id WHERE bci.client_id = " + id + " AND bi.node_id IS NOT NULL)"
}

func bandwagonQuotaCycle(db *gorm.DB) (*model.BandwagonCycle, error) {
	c, err := loadBandwagonConfig()
	if err != nil {
		return nil, err
	}
	if !c.Enabled {
		return nil, nil
	}
	var cycle model.BandwagonCycle
	err = db.Where("ve_id = ?", c.VEID).Order("next_reset DESC").First(&cycle).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &cycle, nil
}

func bandwagonUsedSQL(cycle *model.BandwagonCycle, id string) string {
	return fmt.Sprintf("COALESCE((SELECT SUM(bt.up + bt.down) FROM bandwagon_traffics bt WHERE bt.client_id = %s AND bt.ve_id = '%s' AND bt.minute >= %d AND bt.minute < %d), 0)", id, strings.ReplaceAll(cycle.VEID, "'", "''"), cycle.Start/60*60, cycle.NextReset/60*60)
}

// Keep the operator's Enable flag intact when a cycle quota blocks traffic.
// A persisted block owns only the runtime/traffic gate, so rollover can restore
// exhausted clients without reviving manually disabled or expired accounts.
func (s *InboundService) reconcileBandwagonQuotas(tx *gorm.DB, batch *trafficMutationBatch) error {
	cycle, err := bandwagonQuotaCycle(tx)
	if err != nil || cycle == nil {
		return err
	}
	type quotaRow struct {
		model.ClientRecord
		Used           int64
		TrafficEnabled bool
		BlockedID      int
	}
	var rows []quotaRow
	err = tx.Table("clients c").Select("c.*, " + bandwagonUsedSQL(cycle, "c.id") + " AS used, ct.enable AS traffic_enabled, COALESCE(bq.client_id, 0) AS blocked_id").
		Joins("JOIN client_traffics ct ON ct.email = c.email").Joins("LEFT JOIN bandwagon_quota_blocks bq ON bq.client_id = c.id").Where(bandwagonLocalClientSQL("c.id")).Scan(&rows).Error
	if err != nil {
		return err
	}
	for _, row := range rows {
		exhausted := row.TotalGB > 0 && row.Used >= row.TotalGB
		valid := row.Enable
		if !valid {
			continue
		}
		if exhausted {
			if err := tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&model.BandwagonQuotaBlock{ClientID: row.Id, CycleID: cycle.ID}).Error; err != nil {
				return err
			}
			if !row.TrafficEnabled {
				continue
			}
		} else if row.BlockedID == 0 {
			continue
		}
		// Only our own quota block authorizes an automatic re-enable.
		enable := !exhausted
		if err := tx.Model(&xray.ClientTraffic{}).Where("email = ?", row.Email).Update("enable", enable).Error; err != nil {
			return err
		}
		if enable {
			if err := tx.Delete(&model.BandwagonQuotaBlock{}, "client_id = ?", row.Id).Error; err != nil {
				return err
			}
		}
		var inbounds []model.Inbound
		if err := tx.Model(&model.Inbound{}).Joins("JOIN client_inbounds ci ON ci.inbound_id = inbounds.id").Where("ci.client_id = ? AND inbounds.node_id IS NULL AND inbounds.enable = ?", row.Id, true).Find(&inbounds).Error; err != nil {
			return err
		}
		for _, ib := range inbounds {
			plan := trafficLocalApplyPlan{action: trafficRemoveUser, inbound: ib, email: row.Email}
			if enable {
				var settings map[string]any
				if err := json.Unmarshal([]byte(ib.Settings), &settings); err != nil {
					return err
				}
				cipher, _ := settings["method"].(string)
				clients, _ := settings["clients"].([]any)
				for _, entry := range clients {
					c, ok := entry.(map[string]any)
					if !ok || c["email"] != row.Email {
						continue
					}
					plan.action = trafficAddUser
					plan.client = apiUserFromClient(c, cipher)
					break
				}
				if plan.client == nil {
					return fmt.Errorf("cycle quota client missing from inbound %d", ib.Id)
				}
			}
			batch.localPlans = append(batch.localPlans, plan)
		}
	}
	return nil
}

func bandwagonManagedTrafficSQL() string {
	return "EXISTS (SELECT 1 FROM clients bc WHERE bc.email = client_traffics.email AND " + bandwagonLocalClientSQL("bc.id") + ")"
}

// Like clients with scheduled renewal, cycle-managed clients must not be purged
// merely because their usage exceeded a quota that will renew next period.
func cycleSafePurgeClause(db *gorm.DB) (string, error) {
	cycle, err := bandwagonQuotaCycle(db)
	if err != nil {
		return "", err
	}
	cond := depletedClientsClause
	if cycle != nil {
		cond = strings.Replace(cond, "total > 0 and up + down >= total", "total > 0 and up + down >= total AND NOT ("+bandwagonManagedTrafficSQL()+")", 1)
	}
	return cond, nil
}
