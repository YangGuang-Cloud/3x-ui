// Package dispatcher supplies the optional bandwidth extension compiled into
// the pinned upstream Xray dispatcher by build.py. The upstream module cache
// is never modified.
package dispatcher

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/buf"
	"github.com/xtls/xray-core/common/session"
	"github.com/xtls/xray-core/transport"
	"golang.org/x/time/rate"
)

const rateBurst = 64 * 1024

type clientRate struct {
	UploadMbps   float64 `json:"uploadMbps"`
	DownloadMbps float64 `json:"downloadMbps"`
}
type bandwidthPair struct{ upload, download *rate.Limiter }

var (
	bandwidthUsers   map[string]bandwidthPair
	bandwidthOnce    sync.Once
	bandwidthLoadErr error
)

func loadClientBandwidth() error {
	bandwidthOnce.Do(func() {
		bandwidthUsers = make(map[string]bandwidthPair)
		file := os.Getenv("XUI_CLIENT_RATE_LIMITS_FILE")
		if file == "" {
			return
		}
		body, err := os.ReadFile(file)
		if err != nil {
			bandwidthLoadErr = err
			return
		}
		var limits map[string]clientRate
		if err = json.Unmarshal(body, &limits); err != nil {
			bandwidthLoadErr = err
			return
		}
		for email, limit := range limits {
			if email == "" || limit.UploadMbps < 0 || limit.DownloadMbps < 0 || limit.UploadMbps > 1000000 || limit.DownloadMbps > 1000000 {
				bandwidthLoadErr = fmt.Errorf("invalid client bandwidth configuration")
				return
			}
			bandwidthUsers[email] = bandwidthPair{newBandwidthLimiter(limit.UploadMbps), newBandwidthLimiter(limit.DownloadMbps)}
		}
	})
	return bandwidthLoadErr
}

func newBandwidthLimiter(mbps float64) *rate.Limiter {
	if mbps <= 0 {
		return nil
	}
	return rate.NewLimiter(rate.Limit(mbps*1000000/8), rateBurst)
}

func bandwidthFor(ctx context.Context) bandwidthPair {
	inbound := session.InboundFromContext(ctx)
	if inbound == nil || inbound.User == nil {
		return bandwidthPair{}
	}
	pair := bandwidthUsers[inbound.User.Email]
	if pair.upload != nil || pair.download != nil {
		inbound.CanSpliceCopy = 3
	}
	return pair
}

func limitClientPipes(ctx context.Context, inbound, outbound *transport.Link) {
	pair := bandwidthFor(ctx)
	if pair.upload != nil {
		inbound.Writer = &bandwidthWriter{inbound.Writer, pair.upload, ctx}
	}
	if pair.download != nil {
		outbound.Writer = &bandwidthWriter{outbound.Writer, pair.download, ctx}
	}
}

func limitClientLink(ctx context.Context, link *transport.Link) {
	pair := bandwidthFor(ctx)
	if pair.upload != nil {
		link.Reader = &bandwidthReader{link.Reader, pair.upload, ctx}
	}
	if pair.download != nil {
		link.Writer = &bandwidthWriter{link.Writer, pair.download, ctx}
	}
}

// Each buffer is charged against the same account bucket across every inbound
// and connection. Individual buffers (including UDP datagrams) remain intact.
func waitBandwidth(ctx context.Context, limiter *rate.Limiter, n int) error {
	for n > 0 {
		take := min(n, rateBurst)
		if err := limiter.WaitN(ctx, take); err != nil {
			return err
		}
		n -= take
	}
	return nil
}

type bandwidthWriter struct {
	buf.Writer
	limiter *rate.Limiter
	ctx     context.Context
}

func (w *bandwidthWriter) WriteMultiBuffer(mb buf.MultiBuffer) error {
	for i, b := range mb {
		if b == nil {
			continue
		}
		if err := waitBandwidth(w.ctx, w.limiter, int(b.Len())); err != nil {
			buf.ReleaseMulti(mb[i:])
			return err
		}
		if err := w.Writer.WriteMultiBuffer(buf.MultiBuffer{b}); err != nil {
			buf.ReleaseMulti(mb[i+1:])
			return err
		}
	}
	return nil
}

type bandwidthReader struct {
	buf.Reader
	limiter *rate.Limiter
	ctx     context.Context
}

func (r *bandwidthReader) ReadMultiBuffer() (buf.MultiBuffer, error) {
	mb, err := r.Reader.ReadMultiBuffer()
	if waitErr := waitBandwidth(r.ctx, r.limiter, int(mb.Len())); waitErr != nil {
		buf.ReleaseMulti(mb)
		return nil, waitErr
	}
	return mb, err
}

func (w *bandwidthWriter) Close() error { return common.Close(w.Writer) }
func (w *bandwidthWriter) Interrupt()   { _ = common.Interrupt(w.Writer) }
func (r *bandwidthReader) Close() error { return common.Close(r.Reader) }
func (r *bandwidthReader) Interrupt()   { _ = common.Interrupt(r.Reader) }

// Sniffing uses TimeoutReader after WrapLink; preserve that interface so
// enabling sniffing cannot bypass the limiter or panic on a type assertion.
func (r *bandwidthReader) ReadMultiBufferTimeout(timeout time.Duration) (buf.MultiBuffer, error) {
	reader, ok := r.Reader.(buf.TimeoutReader)
	if !ok {
		return nil, fmt.Errorf("bandwidth reader requires a timeout-aware source")
	}
	mb, err := reader.ReadMultiBufferTimeout(timeout)
	if waitErr := waitBandwidth(r.ctx, r.limiter, int(mb.Len())); waitErr != nil {
		buf.ReleaseMulti(mb)
		return nil, waitErr
	}
	return mb, err
}
