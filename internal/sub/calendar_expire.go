package sub

import (
	"fmt"

	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

func (s *SubService) subscriptionUserinfo(traffic xray.ClientTraffic) string {
	return fmt.Sprintf("upload=%d; download=%d; total=%d; expire=0", traffic.Up, traffic.Down, traffic.Total)
}
