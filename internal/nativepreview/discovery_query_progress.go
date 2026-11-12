package nativepreview

import (
	"context"
	"sync"

	"github.com/yetone/magpie/internal/nativeprepdiag"
)

var queryProgressMarkers = [...]string{
	"MAGPIE_QUERY_V1:entry", "MAGPIE_QUERY_V1:appx", "MAGPIE_QUERY_V1:management",
	"MAGPIE_QUERY_V1:utility", "MAGPIE_QUERY_V1:packages", "MAGPIE_QUERY_V1:path", "MAGPIE_QUERY_V1:json",
}
var queryProgressStages = [...]nativeprepdiag.Stage{
	nativeprepdiag.QueryProgressUnknown, nativeprepdiag.QueryProgressEntry, nativeprepdiag.QueryProgressAppx,
	nativeprepdiag.QueryProgressManagement, nativeprepdiag.QueryProgressUtility, nativeprepdiag.QueryProgressPackages,
	nativeprepdiag.QueryProgressPath, nativeprepdiag.QueryProgressJSON,
}

// queryProgress retains no input bytes. A fixed candidate mask and saturating
// offset recognize complete, ordered lines only. Other stderr is discarded.
// Cmd owns the new stderr pipe/copy goroutine and closes/joins them through the
// original Start/Wait path (also on start failure). No second waiter, authority,
// independent pipe closer, or WaitDelay is introduced; inherited writers can
// still delay that original Wait. Snapshot never waits for EOF or pipe drain.
type queryProgress struct {
	mu      sync.Mutex
	stage   uint8
	invalid bool
	mask    uint8
	offset  uint8
	marker  bool
	cr      bool
}

func (p *queryProgress) Write(data []byte) (int, error) {
	n := len(data)
	// Bound each critical section even if a caller supplies a very large write.
	for len(data) > 0 {
		size := min(len(data), 256)
		p.mu.Lock()
		for _, b := range data[:size] {
			p.accept(b)
		}
		p.mu.Unlock()
		data = data[size:]
	}
	return n, nil
}
func (p *queryProgress) accept(b byte) {
	if b == '\n' {
		if p.marker {
			matched := false
			for i, token := range queryProgressMarkers {
				if p.mask&(1<<i) != 0 && int(p.offset) == len(token) && int(p.stage) == i {
					p.stage++
					matched = true
					break
				}
			}
			if !matched {
				p.invalid = true
			}
		}
		p.mask = 0
		p.offset = 0
		p.marker = false
		p.cr = false
		return
	}
	if p.offset == 0 {
		p.mask = 127
	}
	if b == '\r' && !p.cr {
		p.cr = true
		return
	}
	for i, token := range queryProgressMarkers {
		if p.cr || int(p.offset) >= len(token) || token[p.offset] != b {
			p.mask &^= 1 << i
		}
	}
	if p.offset < 255 {
		p.offset++
	}
	if p.offset == uint8(len("MAGPIE_QUERY_V1:")) && p.mask != 0 {
		p.marker = true
	}
}
func (p *queryProgress) snapshot(ctx context.Context) {
	p.mu.Lock()
	stage := p.stage
	// A malformed marker permanently invalidates this stream. A partial matching
	// line at sampling time cannot stand for a completed boundary.
	if p.invalid || (p.offset > 0 && (p.mask != 0 || p.marker)) || int(stage) >= len(queryProgressStages) {
		stage = 0
	}
	p.mu.Unlock()
	nativeprepdiag.Track(ctx, queryProgressStages[stage]).Complete()
}
