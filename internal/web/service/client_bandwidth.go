package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/json_util"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var clientBandwidthMu sync.Mutex

func validateClientBandwidth(v model.ClientBandwidth) error {
	for _, n := range []float64{v.UploadMbps, v.DownloadMbps} {
		if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 1000000 || (n > 0 && n < 0.01) {
			return errors.New("bandwidth must be 0 (unlimited), or between 0.01 and 1000000 Mbps")
		}
	}
	return nil
}

func (s *ClientService) GetBandwidth(email string) (model.ClientBandwidth, error) {
	client, err := s.GetRecordByEmail(nil, email)
	if err != nil {
		return model.ClientBandwidth{}, err
	}
	value := model.ClientBandwidth{ClientID: client.Id}
	err = database.GetDB().First(&value, "client_id = ?", client.Id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = nil
	}
	return value, err
}

func (s *ClientService) checkBandwidthAttachments(id int) error {
	var inbounds []model.Inbound
	err := database.GetDB().Model(&model.Inbound{}).Joins("JOIN client_inbounds ON client_inbounds.inbound_id = inbounds.id").Where("client_inbounds.client_id = ?", id).Find(&inbounds).Error
	if err != nil {
		return err
	}
	if len(inbounds) == 0 {
		return errors.New("attach this client to a local VLESS inbound before setting bandwidth")
	}
	for _, ib := range inbounds {
		if ib.Protocol != model.VLESS || (ib.NodeID != nil && *ib.NodeID != 0) {
			return errors.New("bandwidth limiting currently supports local VLESS inbounds only")
		}
	}
	return nil
}

func (s *ClientService) SetBandwidth(email string, value model.ClientBandwidth, apply func() error) error {
	if err := validateClientBandwidth(value); err != nil {
		return err
	}
	clientBandwidthMu.Lock()
	defer clientBandwidthMu.Unlock()
	old, err := s.GetBandwidth(email)
	if err != nil {
		return err
	}
	value.ClientID = old.ClientID
	if value.UploadMbps > 0 || value.DownloadMbps > 0 {
		if err := s.checkBandwidthAttachments(value.ClientID); err != nil {
			return err
		}
		if !xray.SupportsClientBandwidth() {
			return errors.New("the running core does not support per-client bandwidth; install the managed SorvoRateV1 core first")
		}
	}
	if old.UploadMbps == value.UploadMbps && old.DownloadMbps == value.DownloadMbps {
		return nil
	}
	db := database.GetDB()
	save := func(v model.ClientBandwidth) error {
		return db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&v).Error
	}
	if err := save(value); err != nil {
		return err
	}
	if err := apply(); err != nil {
		if rollback := save(old); rollback != nil {
			return fmt.Errorf("bandwidth apply failed: %w; database rollback failed: %w", err, rollback)
		}
		if rollback := apply(); rollback != nil {
			return fmt.Errorf("bandwidth apply failed: %w; core rollback failed: %w", err, rollback)
		}
		return err
	}
	return nil
}

func generatedClientBandwidth() (json_util.RawMessage, error) {
	var rows []struct {
		Email        string
		UploadMbps   float64
		DownloadMbps float64
	}
	err := database.GetDB().Table("client_bandwidths AS bandwidth").Select("clients.email, bandwidth.upload_mbps, bandwidth.download_mbps").Joins("JOIN clients ON clients.id = bandwidth.client_id").Where("clients.enable = ? AND (bandwidth.upload_mbps > 0 OR bandwidth.download_mbps > 0)", true).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	limits := map[string]model.ClientBandwidth{}
	for _, r := range rows {
		limits[r.Email] = model.ClientBandwidth{UploadMbps: r.UploadMbps, DownloadMbps: r.DownloadMbps}
	}
	body, err := json.Marshal(limits)
	return json_util.RawMessage(body), err
}
