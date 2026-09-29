// Command relaya forwards webhook events to your local server and sends test
// events, like the Stripe CLI but for every provider Relaya receives.
//
//	relaya login
//	relaya listen --forward-to http://localhost:3000/webhooks
//	relaya trigger payment.captured
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"text/tabwriter"
	"time"

	relaya "github.com/relayaa/relaya-sdks/go"
	"golang.org/x/term"
)

const version = "0.5.0"

const usage = `Relaya CLI %s: forward webhook events to your local server, and send test events.

Usage:
  relaya login [--api-key rk_…] [--base-url URL]   save an API key (admin role)
  relaya logout                                    forget it
  relaya webhooks                                  list your webhooks
  relaya listen --forward-to URL [flags]           forward new events to URL until Ctrl+C
      --webhook ID|name   only this webhook (repeat or comma-separate; default all)
      --events a,b        only these event types
      -H "Name: value"    add a header to every forwarded request (repeatable)
      --print-body        print each event's body
  relaya trigger EVENT_TYPE [--webhook ID|name] [--payload file.json]
                                                   send a signed sample event (the event simulator)
  relaya version

Environment: RELAYA_API_KEY and RELAYA_BASE_URL override the saved login.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdin io.Reader, out io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintf(out, usage, version)
		return nil
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "login":
		return login(ctx, rest, stdin, out)
	case "logout":
		c, err := loadConfig()
		if err != nil {
			return err
		}
		c.APIKey, c.OrgID = "", ""
		if err := saveConfig(c); err != nil {
			return err
		}
		fmt.Fprintln(out, "Logged out.")
		return nil
	case "webhooks":
		return listWebhooks(ctx, out)
	case "listen":
		return listenCmd(ctx, rest, out)
	case "trigger":
		return trigger(ctx, rest, out)
	case "version", "--version", "-v":
		fmt.Fprintln(out, "relaya", version)
		return nil
	case "help", "--help", "-h":
		fmt.Fprintf(out, usage, version)
		return nil
	}
	return fmt.Errorf("unknown command %q (run `relaya help`)", cmd)
}

func login(ctx context.Context, args []string, stdin io.Reader, out io.Writer) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	key := fs.String("api-key", "", "API key (rk_…)")
	base := fs.String("base-url", "", "API base URL")
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := loadConfig()
	if err != nil {
		return err
	}
	if *base != "" {
		c.BaseURL = strings.TrimRight(*base, "/")
	}
	if *key == "" {
		fmt.Fprint(out, "Paste an API key (Relaya → Settings → API keys, role admin); it won't be shown: ")
		// In a terminal, read without echo so the key never appears on screen
		// (or in a screenshot of it). Piped input is read as a line.
		if f, ok := stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
			b, err := term.ReadPassword(int(f.Fd()))
			fmt.Fprintln(out)
			if err != nil {
				return err
			}
			*key = strings.TrimSpace(string(b))
		} else {
			line, err := bufio.NewReader(stdin).ReadString('\n')
			if err != nil && line == "" {
				return errors.New("no API key given")
			}
			*key = strings.TrimSpace(line)
		}
		if *key == "" {
			return errors.New("no API key given")
		}
	}
	c.APIKey, c.OrgID = *key, ""
	cl, err := c.client()
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	org, err := cl.OrgID(cctx)
	if err != nil {
		return fmt.Errorf("that key didn't work: %w", err)
	}
	c.OrgID = org
	if c.ListenSecret == "" {
		c.ListenSecret = newSecret()
	}
	if err := saveConfig(c); err != nil {
		return err
	}
	p, _ := configPath()
	fmt.Fprintf(out, "Logged in to %s (organization %s). Saved to %s\n", c.BaseURL, org, p)
	return nil
}

func listWebhooks(ctx context.Context, out io.Writer) error {
	c, err := loadConfig()
	if err != nil {
		return err
	}
	cl, err := c.client()
	if err != nil {
		return err
	}
	hooks, err := inboundWebhooks(ctx, cl)
	if err != nil {
		return err
	}
	if len(hooks) == 0 {
		fmt.Fprintln(out, "No webhooks yet. Create one in the dashboard.")
		return nil
	}
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tPROVIDER")
	for _, w := range hooks {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", w.ID, w.Name, w.Provider)
	}
	return tw.Flush()
}

// webhookRef is the part of a webhook the CLI needs.
type webhookRef struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Kind     string `json:"kind"`
}

func inboundWebhooks(ctx context.Context, cl *relaya.Client) ([]webhookRef, error) {
	org, err := cl.OrgID(ctx)
	if err != nil {
		return nil, err
	}
	var l struct{ Data []webhookRef }
	if err := cl.Do(ctx, "GET", "/v1/orgs/"+org+"/webhooks", map[string][]string{"kind": {"inbound"}}, nil, &l); err != nil {
		return nil, err
	}
	return l.Data, nil
}

// pickWebhooks resolves --webhook values (IDs or names) to webhooks.
func pickWebhooks(all []webhookRef, refs []string) ([]webhookRef, error) {
	if len(refs) == 0 {
		return all, nil
	}
	var out []webhookRef
	for _, ref := range refs {
		var found *webhookRef
		for i := range all {
			if all[i].ID == ref || strings.EqualFold(all[i].Name, ref) {
				found = &all[i]
				break
			}
		}
		if found == nil {
			return nil, fmt.Errorf("no webhook %q (run `relaya webhooks`)", ref)
		}
		out = append(out, *found)
	}
	return out, nil
}

// splitList reads repeatable, comma-separated flag values.
type splitList []string

func (s *splitList) String() string { return strings.Join(*s, ",") }
func (s *splitList) Set(v string) error {
	for _, x := range strings.Split(v, ",") {
		if x = strings.TrimSpace(x); x != "" {
			*s = append(*s, x)
		}
	}
	return nil
}

// headerList reads repeatable -H "Name: value" flags.
type headerList []string

func (h *headerList) String() string { return strings.Join(*h, "; ") }
func (h *headerList) Set(v string) error {
	if !strings.Contains(v, ":") {
		return fmt.Errorf("header %q: use \"Name: value\"", v)
	}
	*h = append(*h, v)
	return nil
}
