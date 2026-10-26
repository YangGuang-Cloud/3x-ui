package model

// BandwagonCycle is a provider-confirmed accounting window, used by local client quotas.
type BandwagonCycle struct {
	ID            uint    `gorm:"primaryKey" json:"id"`
	VEID          string  `gorm:"index:idx_bwg_cycle,unique" json:"-"`
	NextReset     int64   `gorm:"index:idx_bwg_cycle,unique" json:"nextReset"`
	Start         int64   `json:"start"`
	Partial       bool    `json:"partial"`
	Quota         float64 `json:"quota"`
	Used          float64 `json:"used"`
	Multiplier    float64 `json:"multiplier"`
	UpdatedAtUnix int64   `json:"updatedAt"`
}

// BandwagonTraffic retains local proxy usage in minute buckets across quota resets.
// Stable client IDs prevent renames/deletion/recreation from transferring usage.
type BandwagonTraffic struct {
	VEID     string `gorm:"primaryKey;autoIncrement:false;index:idx_bwg_service_time"`
	ClientID int    `gorm:"primaryKey;autoIncrement:false"`
	Minute   int64  `gorm:"primaryKey;autoIncrement:false;index:idx_bwg_service_time"`
	Up       int64
	Down     int64
}

// BandwagonQuotaBlock distinguishes an automatic cycle quota gate from manual disable.
type BandwagonQuotaBlock struct {
	ClientID int `gorm:"primaryKey;autoIncrement:false"`
	CycleID  uint
}
