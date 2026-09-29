package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	relaya "github.com/Dhirajrai12/relaya-sdks/go"
)

type listenOpts struct {
	cfg       config
	forwardTo string
	webhooks  []string
	events    []string
	headers   http.Header
	printBody bool
	timeout   time.Duration
	// ready is closed once the first connection is live (tests wait on it).
	ready chan struct{}
}

func listenCmd(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("listen", flag.ContinueOnError)
	var hooks, types splitList
	var headers headerList
	forwardTo := fs.String("forward-to", "", "local URL, e.g. http://localhost:3000/webhooks")
	fs.Var(&hooks, "webhook", "webhook ID or name (repeatable)")
	fs.Var(&types, "events", "event types to forward (comma-separated)")
	fs.Var(&headers, "H", `extra header "Name: value" (repeatable)`)
	printBody := fs.Bool("print-body", false, "print each event's body")
	timeout := fs.Duration("timeout", 30*time.Second, "how long the local server may take to answer")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *forwardTo == "" {
		return errors.New("--forward-to is required, e.g. relaya listen --forward-to http://localhost:3000/webhooks")
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	h := http.Header{}
	for _, x := range headers {
		k, v, _ := strings.Cut(x, ":")
		h.Add(strings.TrimSpace(k), strings.TrimSpace(v))
	}
	return listen(ctx, listenOpts{cfg: cfg, forwardTo: *forwardTo, webhooks: hooks, events: types, headers: h, printBody: *printBody, timeout: *timeout}, out)
}

// normalizeTarget accepts "3000", "localhost:3000/hooks" or a full URL.
func normalizeTarget(s string) (string, error) {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "://") {
		// "3000" or "3000/webhooks": a port on this machine.
		port, path, _ := strings.Cut(s, "/")
		if port != "" && strings.Trim(port, "0123456789") == "" {
			s = "localhost:" + port
			if path != "" {
				s += "/" + path
			}
		}
		s = "http://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("--forward-to %q is not a URL like http://localhost:3000/webhooks", s)
	}
	return u.String(), nil
}

// streamURL is the WebSocket for an org: https://host/api → wss://host/api/v1/orgs/{org}/stream.
func streamURL(base, org string) string {
	u := strings.TrimRight(base, "/")
	switch {
	case strings.HasPrefix(u, "https://"):
		u = "wss://" + strings.TrimPrefix(u, "https://")
	case strings.HasPrefix(u, "http://"):
		u = "ws://" + strings.TrimPrefix(u, "http://")
	}
	return u + "/v1/orgs/" + org + "/stream"
}

type listener struct {
	opts   listenOpts
	cl     *relaya.Client
	org    string
	target string
	names  map[string]string // selected webhook ID → name
	hc     *http.Client
	out    io.Writer
	queue  chan string // event IDs to forward, in order
	seen   map[string]bool
	order  []string
	mu     sync.Mutex
	since  time.Time // newest event handled; catch-up starts here (guarded by mu)
}

func listen(ctx context.Context, opts listenOpts, out io.Writer) error {
	cl, err := opts.cfg.client()
	if err != nil {
		return err
	}
	target, err := normalizeTarget(opts.forwardTo)
	if err != nil {
		return err
	}
	org, err := cl.OrgID(ctx)
	if err != nil {
		return err
	}
	all, err := inboundWebhooks(ctx, cl)
	if err != nil {
		return err
	}
	sel, err := pickWebhooks(all, opts.webhooks)
	if err != nil {
		return err
	}
	if len(sel) == 0 {
		return errors.New("no webhooks to listen to: create one in the dashboard first")
	}
	if opts.cfg.ListenSecret == "" {
		opts.cfg.ListenSecret = newSecret()
		_ = saveConfig(opts.cfg)
	}
	if opts.timeout == 0 {
		opts.timeout = 30 * time.Second
	}
	l := &listener{
		opts: opts, cl: cl, org: org, target: target, names: map[string]string{}, out: out,
		hc:    &http.Client{Timeout: opts.timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		queue: make(chan string, 1000), seen: map[string]bool{}, since: time.Now().UTC(),
	}
	var list []string
	for _, w := range sel {
		l.names[w.ID] = w.Name
		list = append(list, fmt.Sprintf("%s (%s)", w.Name, w.Provider))
	}

	fmt.Fprintf(out, "Forwarding events from %s\n  to %s\n", strings.Join(list, ", "), target)
	if len(opts.events) > 0 {
		fmt.Fprintf(out, "  only %s\n", strings.Join(opts.events, ", "))
	}
	fmt.Fprintf(out, "Relaya signs each forwarded request with this secret; verify with it locally\n(e.g. RELAYA_SIGNING_SECRET=%s)\n", opts.cfg.ListenSecret)
	fmt.Fprintln(out, "Press Ctrl+C to stop.")

	go l.worker(ctx)
	first := true
	for backoff := time.Second; ctx.Err() == nil; {
		err := l.connect(ctx, first)
		if ctx.Err() != nil {
			break
		}
		if err == nil {
			backoff = time.Second
		}
		if errors.Is(err, errUnauthorized) {
			return errors.New("the stream refused this API key (revoked, or not a member of the organization)")
		}
		first = false
		fmt.Fprintf(out, "%s  %s; reconnecting in %s… (events meanwhile are caught up)\n", clock(), lostReason(err), backoff)
		select {
		case <-ctx.Done():
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
	fmt.Fprintln(out, "Stopped.")
	return nil
}

var errUnauthorized = errors.New("unauthorized")

// connect runs one WebSocket session until it breaks.
func (l *listener) connect(ctx context.Context, first bool) error {
	dctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	c, _, err := websocket.Dial(dctx, streamURL(l.opts.cfg.BaseURL, l.org), &websocket.DialOptions{
		HTTPHeader: http.Header{"User-Agent": {"Relaya-CLI/" + version}},
	})
	cancel()
	if err != nil {
		return err
	}
	defer c.CloseNow()
	c.SetReadLimit(1 << 20)
	auth, _ := json.Marshal(map[string]string{"type": "auth", "token": l.opts.cfg.APIKey})
	if err := c.Write(ctx, websocket.MessageText, auth); err != nil {
		return err
	}
	for {
		_, raw, err := c.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) == websocket.StatusPolicyViolation {
				return errUnauthorized
			}
			return err
		}
		var m struct {
			Type      string `json:"type"`
			WebhookID string `json:"webhook_id"`
			EventID   string `json:"event_id"`
			Status    string `json:"status"`
		}
		if json.Unmarshal(raw, &m) != nil {
			continue
		}
		switch m.Type {
		case "ready":
			if first {
				fmt.Fprintf(l.out, "%s  Ready. Waiting for events…\n", clock())
				if l.opts.ready != nil {
					close(l.opts.ready)
					l.opts.ready = nil
				}
			} else {
				fmt.Fprintf(l.out, "%s  Reconnected.\n", clock())
				l.catchUp(ctx)
			}
		case "resync":
			l.catchUp(ctx)
		case "event":
			if _, ok := l.names[m.WebhookID]; ok && m.Status == "received" {
				l.enqueue(m.EventID)
			}
		}
	}
}

// catchUp queues events that arrived while the stream was down or lagging.
func (l *listener) catchUp(ctx context.Context) {
	var page struct {
		Data []struct {
			ID         string    `json:"id"`
			WebhookID  string    `json:"webhook_id"`
			ReceivedAt time.Time `json:"received_at"`
		} `json:"data"`
	}
	l.mu.Lock()
	since := l.since
	l.mu.Unlock()
	q := url.Values{"status": {"received"}, "since": {since.Add(-time.Second).Format(time.RFC3339Nano)}, "limit": {"200"}}
	if err := l.cl.Do(ctx, "GET", "/v1/orgs/"+l.org+"/events", q, nil, &page); err != nil {
		fmt.Fprintf(l.out, "%s  couldn't catch up on missed events: %v\n", clock(), err)
		return
	}
	for i := len(page.Data) - 1; i >= 0; i-- { // oldest first
		if _, ok := l.names[page.Data[i].WebhookID]; ok {
			l.enqueue(page.Data[i].ID)
		}
	}
}

func (l *listener) enqueue(id string) {
	if l.seen[id] {
		return
	}
	l.seen[id] = true
	l.order = append(l.order, id)
	if len(l.order) > 5000 { // forget the oldest
		delete(l.seen, l.order[0])
		l.order = l.order[1:]
	}
	select {
	case l.queue <- id:
	default:
		fmt.Fprintf(l.out, "%s  too many events queued; skipped %s\n", clock(), id)
	}
}

// worker forwards queued events one at a time, in arrival order.
func (l *listener) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case id := <-l.queue:
			l.handle(ctx, id)
		}
	}
}

func (l *listener) handle(ctx context.Context, id string) {
	var ev RawEvent
	if err := l.cl.Do(ctx, "GET", "/v1/orgs/"+l.org+"/events/"+id+"/raw", nil, nil, &ev); err != nil {
		var e *relaya.Error
		if errors.As(err, &e) && (e.Status == 403 || e.Status == 404) {
			fmt.Fprintf(l.out, "%s  can't read event %s: `relaya listen` needs an API key with the admin role\n", clock(), id)
			return
		}
		fmt.Fprintf(l.out, "%s  can't read event %s: %v\n", clock(), id, err)
		return
	}
	l.mu.Lock()
	if ev.ReceivedAt.After(l.since) {
		l.since = ev.ReceivedAt
	}
	l.mu.Unlock()
	if len(l.opts.events) > 0 && !slices.Contains(l.opts.events, ev.Type) {
		return
	}
	req, _, err := buildForward(ctx, l.target, ev, l.opts.cfg.ListenSecret, l.opts.headers, time.Now())
	if err != nil {
		fmt.Fprintf(l.out, "%s  %v\n", clock(), err)
		return
	}
	res := forward(l.hc, req)
	label := ev.Type
	if label == "" {
		label = "(no type)"
	}
	if ev.Simulated {
		label += " (simulated)"
	}
	if len(l.names) > 1 {
		label = l.names[ev.WebhookID] + ": " + label
	}
	switch {
	case res.Err != nil:
		fmt.Fprintf(l.out, "%s  %s  %s  → failed: %v\n", clock(), label, short(ev.ID), res.Err)
	default:
		mark := "✓"
		if res.Status >= 300 {
			mark = "✗"
		}
		fmt.Fprintf(l.out, "%s  %s  %s  → %s %d %s (%d ms)\n", clock(), label, short(ev.ID), mark, res.Status, http.StatusText(res.Status), res.Duration.Milliseconds())
		if res.Status >= 300 && res.Body != "" {
			fmt.Fprintf(l.out, "      %s\n", oneLine(res.Body, 300))
		}
	}
	if l.opts.printBody {
		fmt.Fprintf(l.out, "      %s\n", oneLine(string(ev.Body), 2000))
	}
}

func clock() string { return time.Now().Format("15:04:05") }

// lostReason says in a few words why the stream went away.
func lostReason(err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "refused") || strings.Contains(s, "no such host") || strings.Contains(s, "handshake"):
		return "can't reach Relaya"
	case strings.Contains(s, "timeout") || strings.Contains(s, "deadline"):
		return "Relaya didn't answer in time"
	}
	return "connection to Relaya closed"
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > max {
		s = s[:max] + "…"
	}
	return s
}
