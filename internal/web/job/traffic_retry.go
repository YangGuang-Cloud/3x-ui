package job

import (
	"sync"

	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

// Traffic collectors advance their delta baselines before SQLite commits. Retain
// failed batches across polls; successful outbound accounting and live speed
// samples still consume only the fresh poll, never these retries.
type trafficAccountingRetry struct {
	mu       sync.Mutex
	inbounds map[string]*xray.Traffic
	clients  map[string]*xray.ClientTraffic
}

func (r *trafficAccountingRetry) commit(in []*xray.Traffic, clients []*xray.ClientTraffic, save func([]*xray.Traffic, []*xray.ClientTraffic) (bool, bool, error)) (bool, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.inbounds == nil {
		r.inbounds = make(map[string]*xray.Traffic)
		r.clients = make(map[string]*xray.ClientTraffic)
	}
	for _, t := range in {
		if t == nil || !t.IsInbound {
			continue
		}
		if old := r.inbounds[t.Tag]; old != nil {
			old.Up += t.Up
			old.Down += t.Down
		} else {
			copy := *t
			r.inbounds[t.Tag] = &copy
		}
	}
	for _, t := range clients {
		if t == nil {
			continue
		}
		if old := r.clients[t.Email]; old != nil {
			old.Up += t.Up
			old.Down += t.Down
		} else {
			copy := *t
			r.clients[t.Email] = &copy
		}
	}
	a := make([]*xray.Traffic, 0, len(r.inbounds))
	for _, v := range r.inbounds {
		a = append(a, v)
	}
	b := make([]*xray.ClientTraffic, 0, len(r.clients))
	for _, v := range r.clients {
		b = append(b, v)
	}
	restart, disabled, err := save(a, b)
	if err == nil {
		clear(r.inbounds)
		clear(r.clients)
	}
	return restart, disabled, err
}
