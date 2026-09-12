// Package listen forwards live events to a local port.
//
// The connection is opened OUTBOUND from this machine. That is what removes
// the need for a tunnel: no public URL, no inbound port, and it works behind
// NAT and corporate firewalls because it is an ordinary outgoing HTTPS
// connection.
//
// Shape of the loop: create a session over HTTP, dial the socket, and for each
// frame that arrives POST it to the local target and report the result back up
// the socket. Reconnect with backoff when the socket drops, because laptops
// sleep, wifi flaps, and the server cycles connections on deploy.
package listen

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/bachsdev/bachs-cli/internal/api"
	"github.com/bachsdev/bachs-cli/internal/config"
)

const (
	// Fallback if the server does not say how often to cycle.
	defaultReconnectSeconds = 60

	// Reconnect backoff. Starts fast because most drops are transient — a
	// deploy, a brief network blip — and someone watching a terminal should
	// see it come back immediately. Caps low so a laptop waking from sleep
	// reconnects within seconds rather than sitting idle.
	backoffInitial = 1 * time.Second
	backoffMax     = 15 * time.Second
	backoffFactor  = 2

	// A local handler gets this long before we give up on it. Generous: a
	// developer may well be sitting on a breakpoint.
	localTimeout = 30 * time.Second
)

// ANSI colours. Disabled when NO_COLOR is set or stdout is not a terminal,
// so piping to a file or a log collector does not fill it with escapes.
var (
	colGreen  = "\033[32m"
	colRed    = "\033[31m"
	colYellow = "\033[33m"
	colDim    = "\033[2m"
	colBold   = "\033[1m"
	colReset  = "\033[0m"
)

// Exported so other commands render the same way. They are set once in init(),
// after the NO_COLOR and terminal checks, so every command agrees on whether
// output is coloured rather than each deciding for itself.
var (
	Green  = ""
	Red    = ""
	Yellow = ""
	Dim    = ""
	Bold   = ""
	Reset  = ""
)

func init() {
	if os.Getenv("NO_COLOR") != "" || !isTerminal(os.Stdout) {
		colGreen, colRed, colYellow, colDim, colBold, colReset = "", "", "", "", "", ""
	}
	Green, Red, Yellow = colGreen, colRed, colYellow
	Dim, Bold, Reset = colDim, colBold, colReset
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// Frame is what the server pushes down the socket.
type Frame struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	AttemptID string `json:"attempt_id"`
	EventID   string `json:"event_id"`
	EventType string `json:"event_type"`
	Payload   string `json:"payload"`
	Timestamp int64  `json:"timestamp"`
	Signature string `json:"signature"`
}

// result is what we send back, so the delivery shows up in the dashboard with
// what the local handler actually said.
type result struct {
	Type       string  `json:"type"`
	EventID    string  `json:"event_id"`
	AttemptID  string  `json:"attempt_id"`
	Status     int     `json:"status"`
	DurationMS float64 `json:"duration_ms"`
	Error      string  `json:"error,omitempty"`
}

type Options struct {
	ForwardTo  string
	Events     []string
	DeviceName string
}

// normaliseTarget accepts the shapes people actually type.
//
// "localhost:3000/webhooks", ":3000/webhooks" and a full URL should all work.
// Requiring a scheme for a local address is pointless friction.
func normaliseTarget(forwardTo string) string {
	t := strings.TrimSpace(forwardTo)
	if strings.HasPrefix(t, "http://") || strings.HasPrefix(t, "https://") {
		return t
	}
	if strings.HasPrefix(t, ":") {
		return "http://localhost" + t
	}
	return "http://" + t
}

// Run creates a session and forwards until interrupted.
func Run(ctx context.Context, cfg config.Config, opts Options) error {
	target := normaliseTarget(opts.ForwardTo)
	client := api.New(cfg)

	session, err := client.CreateSession(ctx, api.CreateSessionRequest{
		DeviceName: opts.DeviceName,
		ForwardTo:  opts.ForwardTo,
		Events:     opts.Events,
	})
	if err != nil {
		return fmt.Errorf("could not create session: %w", err)
	}

	// Release the session on the way out so fanout stops aiming at a socket
	// nobody holds. The server's reaper covers a hard kill.
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = client.CloseSession(closeCtx, session.SessionID)
	}()

	env := "sandbox"
	if !cfg.IsSandbox() {
		env = colYellow + "live" + colReset
	}
	fmt.Printf("%sReady!%s Forwarding %s events to %s\n", colBold, colReset, env, target)

	// The secret goes to stderr, not stdout. `bachs listen > session.log` in CI
	// would otherwise write a live signing secret into a build artifact that
	// gets archived and shared. A developer at a terminal sees it either way.
	fmt.Fprintf(
		os.Stderr,
		"Your webhook signing secret is %s%s%s (^C to quit)\n",
		colBold, session.SigningSecret, colReset,
	)
	fmt.Printf(
		"%sSession %s · %d event type(s)%s\n\n",
		colDim, session.SessionID, len(session.Events), colReset,
	)

	cycleAfter := time.Duration(session.ReconnectAfterSeconds) * time.Second
	if cycleAfter <= 0 {
		cycleAfter = defaultReconnectSeconds * time.Second
	}

	local := &http.Client{
		Timeout: localTimeout,
		// A local handler should be its own final destination. Following a
		// redirect would silently POST the payload somewhere unvalidated.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	backoff := backoffInitial
	for {
		if ctx.Err() != nil {
			return nil
		}

		err := runSocket(ctx, cfg, session, target, local, cycleAfter)
		switch {
		case ctx.Err() != nil:
			return nil
		case err == nil:
			// A clean cycle is expected, not a failure — reset the backoff so
			// the next real drop reconnects fast.
			backoff = backoffInitial
		default:
			fmt.Printf(
				"%s⟳%s %sconnection lost (%v); reconnecting in %s%s\n",
				colYellow, colReset, colDim, err, backoff.Round(time.Second), colReset,
			)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(backoff):
			}
			backoff *= backoffFactor
			if backoff > backoffMax {
				backoff = backoffMax
			}
		}
	}
}

// runSocket is one connection's lifetime. Returns nil when it is time to
// cycle, which the caller treats as expected rather than an error.
func runSocket(
	ctx context.Context,
	cfg config.Config,
	session *api.CreateSessionResponse,
	target string,
	local *http.Client,
	cycleAfter time.Duration,
) error {
	// Cycle deliberately rather than holding one socket forever, so a server
	// deploy draining connections does not strand this session.
	sockCtx, cancel := context.WithTimeout(ctx, cycleAfter)
	defer cancel()

	url := cfg.WebSocketURL() + "?token=" + session.Token
	conn, _, err := websocket.Dial(sockCtx, url, nil)
	if err != nil {
		return err
	}
	defer conn.CloseNow()

	// Frames carry a full event payload, which can be well over the default.
	conn.SetReadLimit(8 << 20)

	for {
		_, raw, err := conn.Read(sockCtx)
		if err != nil {
			// The deadline firing is the planned cycle, not a fault.
			if sockCtx.Err() != nil && ctx.Err() == nil {
				return nil
			}
			return err
		}

		var frame Frame
		if err := json.Unmarshal(raw, &frame); err != nil {
			continue // not something we understand; ignore rather than die
		}
		if frame.Type != "webhook.forward" {
			continue // "ready", "pong", anything future
		}

		status, duration, forwardErr := forward(sockCtx, local, target, frame)
		report(frame, status, duration, forwardErr)

		res := result{
			Type:       "delivery_result",
			EventID:    frame.EventID,
			AttemptID:  frame.AttemptID,
			Status:     status,
			DurationMS: float64(duration.Microseconds()) / 1000.0,
		}
		if forwardErr != nil {
			res.Error = forwardErr.Error()
		}
		encoded, err := json.Marshal(res)
		if err != nil {
			continue
		}
		if err := conn.Write(sockCtx, websocket.MessageText, encoded); err != nil {
			// The socket is going away; let the caller reconnect.
			if sockCtx.Err() != nil && ctx.Err() == nil {
				return nil
			}
			return err
		}
	}
}

// forward POSTs one event to the local handler.
//
// The signature header is passed through exactly as the server built it, so
// the developer's verification code sees a genuinely signed request.
func forward(
	ctx context.Context, client *http.Client, target string, frame Frame,
) (int, time.Duration, error) {
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, target, bytes.NewReader([]byte(frame.Payload)),
	)
	if err != nil {
		return 0, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Bachs-CLI-Listen/"+api.Version)
	req.Header.Set("X-Bachs-Timestamp", strconv.FormatInt(frame.Timestamp, 10))
	if frame.Signature != "" {
		req.Header.Set("X-Bachs-Signature-V2", frame.Signature)
	}

	started := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(started)
	if err != nil {
		return 0, elapsed, err
	}
	defer resp.Body.Close()
	// Drain so the connection can be reused for the next event.
	_, _ = io.Copy(io.Discard, resp.Body)

	return resp.StatusCode, elapsed, nil
}

func report(frame Frame, status int, duration time.Duration, err error) {
	ms := float64(duration.Microseconds()) / 1000.0

	if err != nil {
		fmt.Printf(
			"%s✗%s %s %s%s%s %s%v%s %s[%.0fms]%s\n",
			colRed, colReset, frame.EventType,
			colDim, frame.EventID, colReset,
			colRed, err, colReset,
			colDim, ms, colReset,
		)
		return
	}

	mark, colour := "✓", colGreen
	if status < 200 || status >= 300 {
		mark, colour = "✗", colRed
	}
	fmt.Printf(
		"%s%s%s %s %s%s%s %s%d%s %s[%.0fms]%s\n",
		colour, mark, colReset, frame.EventType,
		colDim, frame.EventID, colReset,
		colour, status, colReset,
		colDim, ms, colReset,
	)
}
