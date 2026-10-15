// Package aerospace is a thin client over the `aerospace` CLI and its event
// stream. All window knowledge comes from AeroSpace; aerocycle never talks to
// the Accessibility API directly.
package aerospace

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// Window mirrors the JSON produced by list-windows with our --format.
type Window struct {
	ID         int    `json:"window-id"`
	BundleID   string `json:"app-bundle-id"`
	AppName    string `json:"app-name"`
	Workspace  string `json:"workspace"`
	Title      string `json:"window-title"`
	Fullscreen bool   `json:"window-is-fullscreen"`
}

// listFormat selects exactly the fields we need. Each %{...} becomes a JSON key.
const listFormat = "%{window-id}%{app-bundle-id}%{app-name}%{workspace}%{window-title}%{window-is-fullscreen}"

// Client runs AeroSpace commands.
type Client struct {
	bin string
}

// New locates the `aerospace` binary on PATH.
func New() (*Client, error) {
	bin, err := exec.LookPath("aerospace")
	if err != nil {
		return nil, fmt.Errorf("`aerospace` not found on PATH: %w", err)
	}
	return &Client{bin: bin}, nil
}

func (c *Client) run(args ...string) ([]byte, error) {
	cmd := exec.Command(c.bin, args...)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("aerospace %s: %s", strings.Join(args, " "), strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, err
	}
	return out, nil
}

// ListAll returns every managed window across all monitors/workspaces.
func (c *Client) ListAll() ([]Window, error) {
	out, err := c.run("list-windows", "--all", "--json", "--format", listFormat)
	if err != nil {
		return nil, err
	}
	var ws []Window
	if err := json.Unmarshal(out, &ws); err != nil {
		return nil, fmt.Errorf("decode list-windows: %w", err)
	}
	return ws, nil
}

// ListForBundle returns the managed windows belonging to one app.
func (c *Client) ListForBundle(bundle string) ([]Window, error) {
	all, err := c.ListAll()
	if err != nil {
		return nil, err
	}
	var ws []Window
	for _, w := range all {
		if w.BundleID == bundle {
			ws = append(ws, w)
		}
	}
	return ws, nil
}

// Focused returns the currently focused window.
func (c *Client) Focused() (Window, error) {
	out, err := c.run("list-windows", "--focused", "--json", "--format", listFormat)
	if err != nil {
		return Window{}, err
	}
	var ws []Window
	if err := json.Unmarshal(out, &ws); err != nil {
		return Window{}, fmt.Errorf("decode focused: %w", err)
	}
	if len(ws) == 0 {
		return Window{}, fmt.Errorf("no focused window")
	}
	return ws[0], nil
}

// FocusWindow focuses a window by its AeroSpace window id.
func (c *Client) FocusWindow(id int) error {
	_, err := c.run("focus", "--window-id", fmt.Sprint(id))
	return err
}

// Workspace focuses a workspace, making it visible.
func (c *Client) Workspace(name string) error {
	_, err := c.run("workspace", name)
	return err
}

// Eval runs an AeroSpace command expression exactly as `aerospace eval` does
// (e.g. "move left", "layout tiles horizontal vertical", "a; b").
func (c *Client) Eval(expr string) error {
	_, err := c.run("eval", expr)
	return err
}

// Event is one line of `aerospace subscribe`.
type Event struct {
	Event         string `json:"_event"`
	WindowID      int    `json:"windowId"`
	Workspace     string `json:"workspace"`
	PrevWorkspace string `json:"prevWorkspace"`
	Mode          string `json:"mode"`
	AppBundleID   string `json:"appBundleId"`
	AppName       string `json:"appName"`
	MonitorID     int    `json:"monitorId"`
}

// Subscribe starts `aerospace subscribe --all` and streams decoded events.
// The returned error channel receives a non-nil error when the stream drops.
func (c *Client) Subscribe(ctx context.Context) (<-chan Event, <-chan error, error) {
	cmd := exec.CommandContext(ctx, c.bin, "subscribe", "--all")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}

	events := make(chan Event, 64)
	errs := make(chan error, 1)

	go func() {
		defer close(events)
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			var ev Event
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				continue // tolerate unknown/partial lines
			}
			select {
			case events <- ev:
			case <-ctx.Done():
				return
			}
		}
		if err := sc.Err(); err != nil {
			errs <- err
		} else {
			errs <- fmt.Errorf("subscribe stream ended")
		}
		_ = cmd.Wait()
	}()

	return events, errs, nil
}
