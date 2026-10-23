package dispatcher

import (
	"context"
	"testing"
	"time"

	"github.com/xtls/xray-core/common/buf"
	"golang.org/x/time/rate"
)

type recordingWriter struct {
	sizes               []int32
	closed, interrupted bool
}

func (w *recordingWriter) WriteMultiBuffer(mb buf.MultiBuffer) error {
	for _, b := range mb {
		w.sizes = append(w.sizes, b.Len())
	}
	buf.ReleaseMulti(mb)
	return nil
}
func (w *recordingWriter) Close() error { w.closed = true; return nil }
func (w *recordingWriter) Interrupt()   { w.interrupted = true }
func TestBandwidthPreservesPacketsAndLifecycle(t *testing.T) {
	base := &recordingWriter{}
	writer := &bandwidthWriter{base, rate.NewLimiter(rate.Inf, rateBurst), context.Background()}
	mb := buf.MultiBuffer{buf.FromBytes([]byte("packet-one")), buf.FromBytes([]byte("two"))}
	if err := writer.WriteMultiBuffer(mb); err != nil {
		t.Fatal(err)
	}
	if len(base.sizes) != 2 || base.sizes[0] != 10 || base.sizes[1] != 3 {
		t.Fatalf("packets changed: %v", base.sizes)
	}
	writer.Interrupt()
	_ = writer.Close()
	if !base.closed || !base.interrupted {
		t.Fatal("lost stream lifecycle")
	}
}

func TestBandwidthCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	limiter := newBandwidthLimiter(1)
	limiter.AllowN(time.Now(), rateBurst)
	if err := waitBandwidth(ctx, limiter, 1); err == nil {
		t.Fatal("cancelled stream waited or succeeded")
	}
}
