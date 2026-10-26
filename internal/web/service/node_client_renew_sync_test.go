package service

import (
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

const (
	renewFirstExpiry  = int64(1893456000000)
	renewSecondExpiry = renewFirstExpiry + int64(2592000000)
	renewPeriodDays   = 30
)

// TestNodeRenew_ResetsMasterTraffic reproduces #5843: a node-side auto-renew
// extends the deadline and resets the node's counters, and the master must
// start a fresh quota window instead of keeping the old period's usage.

// TestNodeRenew_ReenablesDepletedClient: the node re-enables a client it
// renews; the master's depleted-disable must lift with the fresh window.

// TestNodeRenew_ClearsGlobalTraffic mirrors autoRenewClients: stale cross-panel
// pushes for the renewed email must not re-deplete the fresh window.

// TestNodeCounterDip_SameExpiry_KeepsTraffic: a plain counter dip (#5456) with
// no deadline movement stays on the clamp path even for a renewable client.
func TestNodeCounterDip_SameExpiry_KeepsTraffic(t *testing.T) {
	db := initTrafficTestDB(t)
	createNodeInbound(t, db, 1, "n1-in", 41001)
	svc := &InboundService{}

	const email = "dip-only"
	syncNode(t, svc, 1, "n1-in", xray.ClientTraffic{Email: email, Up: 0, Down: 0, ExpiryTime: renewFirstExpiry, Reset: renewPeriodDays, Enable: true})
	syncNode(t, svc, 1, "n1-in", xray.ClientTraffic{Email: email, Up: 950, Down: 150, ExpiryTime: renewFirstExpiry, Reset: renewPeriodDays, Enable: true})
	syncNode(t, svc, 1, "n1-in", xray.ClientTraffic{Email: email, Up: 50, Down: 10, ExpiryTime: renewFirstExpiry, Reset: renewPeriodDays, Enable: true})
	assertUpDown(t, readTraffic(t, db, email), 950, 150, "dip without renewal")
}

// TestNodeExpiryAdvance_RisingCounters_Accumulates: a manual deadline extension
// while traffic keeps flowing is not a renewal and must keep accumulating.
func TestNodeExpiryAdvance_RisingCounters_Accumulates(t *testing.T) {
	db := initTrafficTestDB(t)
	createNodeInbound(t, db, 1, "n1-in", 41001)
	svc := &InboundService{}

	const email = "extend-only"
	syncNode(t, svc, 1, "n1-in", xray.ClientTraffic{Email: email, Up: 0, Down: 0, ExpiryTime: renewFirstExpiry, Reset: renewPeriodDays, Enable: true})
	syncNode(t, svc, 1, "n1-in", xray.ClientTraffic{Email: email, Up: 100, Down: 50, ExpiryTime: renewFirstExpiry, Reset: renewPeriodDays, Enable: true})
	syncNode(t, svc, 1, "n1-in", xray.ClientTraffic{Email: email, Up: 160, Down: 90, ExpiryTime: renewSecondExpiry, Reset: renewPeriodDays, Enable: true})
	assertUpDown(t, readTraffic(t, db, email), 160, 90, "extension while accumulating")
}
