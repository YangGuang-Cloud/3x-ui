package model

// ClientBandwidth is local to this panel. Identity is the database client ID,
// so renaming an email preserves limits and recreating a client never inherits them.
type ClientBandwidth struct {
	ClientID     int     `json:"-" gorm:"primaryKey;autoIncrement:false;column:client_id"`
	UploadMbps   float64 `json:"uploadMbps" gorm:"not null;default:0"`
	DownloadMbps float64 `json:"downloadMbps" gorm:"not null;default:0"`
}
