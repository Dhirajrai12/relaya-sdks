package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// trigger sends a sample event through the event simulator: signed with the
// webhook's own secret as the provider signs it, then stored and forwarded like
// a real one (and picked up by `relaya listen`).
func trigger(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("trigger", flag.ContinueOnError)
	var hooks splitList
	fs.Var(&hooks, "webhook", "webhook ID or name")
	payloadFile := fs.String("payload", "", "send this file instead of the sample (JSON; form-encoded for PayU)")
	// Let flags come after the event type too: relaya trigger payment.captured --webhook Payments
	var eventType string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		eventType, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if eventType == "" && fs.NArg() > 0 {
		eventType = fs.Arg(0)
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	cl, err := cfg.client()
	if err != nil {
		return err
	}
	all, err := inboundWebhooks(ctx, cl)
	if err != nil {
		return err
	}
	sel, err := pickWebhooks(all, hooks)
	if err != nil {
		return err
	}
	switch {
	case len(sel) == 0:
		return errors.New("no webhooks yet: create one in the dashboard first")
	case len(sel) > 1:
		var names []string
		for _, w := range sel {
			names = append(names, fmt.Sprintf("%q (%s)", w.Name, w.Provider))
		}
		return fmt.Errorf("which webhook? add --webhook with one of: %s", strings.Join(names, ", "))
	}
	w := sel[0]
	body := map[string]string{"event_type": eventType}
	if *payloadFile != "" {
		b, err := os.ReadFile(*payloadFile)
		if err != nil {
			return err
		}
		body["payload"] = string(b)
	}
	org, err := cl.OrgID(ctx)
	if err != nil {
		return err
	}
	var res struct {
		ID         string `json:"id"`
		Duplicate  bool   `json:"duplicate"`
		EventType  string `json:"event_type"`
		Status     string `json:"status"`
		Signature  string `json:"signature"`
		Deliveries int    `json:"deliveries"`
	}
	if err := cl.Do(ctx, "POST", "/v1/orgs/"+org+"/webhooks/"+w.ID+"/simulate", nil, body, &res); err != nil {
		return err
	}
	switch {
	case res.Status != "received":
		fmt.Fprintf(out, "Rejected by %s: signature %s (a real event like this would get HTTP 401). Event %s\n", w.Name, res.Signature, res.ID)
	case res.Duplicate:
		fmt.Fprintf(out, "Duplicate: %s already received an event with this ID, so nothing new was sent. Event %s\n", w.Name, res.ID)
	default:
		fmt.Fprintf(out, "Sent %s to %s (signature %s), forwarded to %d destination(s). Event %s\n",
			res.EventType, w.Name, res.Signature, res.Deliveries, res.ID)
	}
	return nil
}
