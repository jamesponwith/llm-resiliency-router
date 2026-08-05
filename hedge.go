package main

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// attemptResult is one upstream attempt in a hedged race.
type attemptResult struct {
	i    int
	resp *http.Response
	err  error
	dur  time.Duration
}

func (a attemptResult) ok() bool {
	return a.err == nil && a.resp != nil &&
		a.resp.StatusCode < 500 && a.resp.StatusCode != http.StatusTooManyRequests
}

// peekedBody re-attaches the bytes a bufio.Reader buffered while waiting for
// the first token to the response's original closer.
type peekedBody struct {
	io.Reader
	io.Closer
}

// attempt sends the request to upstream i and waits for its first body byte —
// "first token", the signal hedging races on. Hard failures are recorded
// against the cell unless the context was cancelled: a loser we killed
// didn't fail, we abandoned it.
func (rt *router) attempt(ctx context.Context, i int, r *http.Request, body []byte, meta chatMeta) attemptResult {
	u, cell := rt.cfg.Upstreams[i], rt.cells[i]
	start := time.Now()
	resp, err := adapterFor(u).roundTrip(rt.client, r.WithContext(ctx), u, body, meta)
	res := attemptResult{i: i, resp: resp, err: err, dur: time.Since(start)}
	if res.ok() {
		br := bufio.NewReader(resp.Body)
		if _, perr := br.Peek(1); perr != nil && perr != io.EOF { // EOF = empty body, still a response
			resp.Body.Close()
			res.resp, res.err = nil, perr
		} else {
			resp.Body = peekedBody{br, resp.Body}
			res.dur = time.Since(start)
		}
	}
	if !res.ok() && ctx.Err() == nil {
		status := 0
		if res.resp != nil {
			status = res.resp.StatusCode
		}
		cell.Record(time.Since(start), true)
		slog.Warn("upstream hard failure", "upstream", u.Name, "cell", cell.State().String(),
			"status", status, "err", res.err)
	}
	return res
}

// hedged races upstream `first` against `second`, which starts hedge_after
// later — or immediately if first hard-fails before the timer. The first
// usable response wins and the loser's request is cancelled. Returns the
// winning (or final failing) result and whether the hedge fired.
func (rt *router) hedged(r *http.Request, body []byte, meta chatMeta, first, second int) (attemptResult, bool) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel() // kills the loser still in flight
	ch := make(chan attemptResult, 2)
	launch := func(i int) { go func() { ch <- rt.attempt(ctx, i, r, body, meta) }() }
	launch(first)
	timer := time.NewTimer(time.Duration(rt.cfg.HedgeAfter))
	defer timer.Stop()
	launched, received, fired := 1, 0, false
	// drain closes bodies of results that arrive after we've returned
	// (e.g. a slow loser completing rather than seeing the cancel).
	drain := func() {
		left := launched - received
		go func() {
			for ; left > 0; left-- {
				if res := <-ch; res.resp != nil {
					res.resp.Body.Close()
				}
			}
		}()
	}
	for {
		select {
		case res := <-ch:
			received++
			if res.ok() || received == 2 {
				drain()
				return res, fired
			}
			if !fired { // primary died before the timer: hedge now
				launch(second)
				launched, fired = 2, true
			}
		case <-timer.C:
			if !fired {
				launch(second)
				launched, fired = 2, true
			}
		}
	}
}
