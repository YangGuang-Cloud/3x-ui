package service

type ClientRenewalPreviewRequest struct {
	ExpiryTime   int64 `json:"expiryTime" example:"1893456000000"`
	Reset        int   `json:"reset" example:"0"`
	ResetDay     int   `json:"resetDay" example:"1"`
	ResetWeekday int   `json:"resetWeekday" example:"0"`
	ResetMax     int   `json:"resetMax" example:"0"`
	ResetCount   int   `json:"resetCount" example:"0"`
}

type ClientRenewalPreview struct {
	TimeZone            string `json:"timeZone" example:"UTC"`
	RenewAt             string `json:"renewAt" example:"2030-01-01T00:00:00Z"`
	ValidThrough        string `json:"validThrough" example:"2029-12-31T23:59:59Z"`
	NextExpiry          string `json:"nextExpiry" example:"2030-02-01T00:00:00Z"`
	SuggestedExpiryTime int64  `json:"suggestedExpiryTime" example:"1893456000000"`
	SuggestedExpiry     string `json:"suggestedExpiry" example:"2030-01-01T00:00:00Z"`
	Renewals            int    `json:"renewals" example:"1"`
	CanRenew            bool   `json:"canRenew" example:"true"`
	DelayedStart        bool   `json:"delayedStart" example:"false"`
}
