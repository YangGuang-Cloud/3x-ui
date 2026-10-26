package job

import (
	"errors"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

func TestTrafficAccountingRetryDoesNotLoseOrDuplicateDeltas(t *testing.T) {
	var retry trafficAccountingRetry
	original := &xray.ClientTraffic{Email: "a", Up: 10, Down: 20}
	_, _, err := retry.commit([]*xray.Traffic{{IsInbound: true, Tag: "in", Up: 10}, {IsOutbound: true, Tag: "out", Up: 10}}, []*xray.ClientTraffic{original}, func(a []*xray.Traffic, b []*xray.ClientTraffic) (bool, bool, error) {
		return false, false, errors.New("database unavailable")
	})
	if err == nil {
		t.Fatal("expected failure")
	}
	_, _, err = retry.commit([]*xray.Traffic{{IsInbound: true, Tag: "in", Up: 5}}, []*xray.ClientTraffic{{Email: "a", Up: 5, Down: 7}}, func(a []*xray.Traffic, b []*xray.ClientTraffic) (bool, bool, error) {
		if len(a) != 1 || a[0].Up != 15 || len(b) != 1 || b[0].Up != 15 || b[0].Down != 27 {
			t.Fatalf("lost or duplicated usage: %+v %+v", a, b)
		}
		return false, false, nil
	})
	if err != nil || original.Up != 10 || original.Down != 20 {
		t.Fatal("retry altered live speed sample", err)
	}
	retry.commit(nil, nil, func(a []*xray.Traffic, b []*xray.ClientTraffic) (bool, bool, error) {
		if len(a) != 0 || len(b) != 0 {
			t.Fatal("successful batch replayed")
		}
		return false, false, nil
	})
}
