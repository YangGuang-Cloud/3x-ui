package controller

import (
	"github.com/gin-gonic/gin"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

func NewBandwagonController(api *gin.RouterGroup) {
	api.GET("/bandwagon", func(c *gin.Context) { v, e := service.GetBandwagonStatus(); jsonObj(c, v, e) })
	api.POST("/bandwagon/config", func(c *gin.Context) {
		var v service.BandwagonConfig
		if e := c.ShouldBindJSON(&v); e != nil {
			jsonMsg(c, "配置格式错误", e)
			return
		}
		if e := service.SaveBandwagonConfig(v); e != nil {
			jsonMsg(c, "保存失败", e)
			return
		}
		// Saving credentials is independent of temporary provider availability.
		jsonObj(c, nil, nil)
	})
	api.POST("/bandwagon/sync", func(c *gin.Context) {
		if e := service.SyncBandwagon(); e != nil {
			jsonMsg(c, "同步失败", e)
			return
		}
		v, e := service.GetBandwagonStatus()
		jsonObj(c, v, e)
	})
}
