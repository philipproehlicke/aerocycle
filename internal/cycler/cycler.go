// Package cycler tracks which window of each application was used most
// recently and implements "press once for the last-used window, press again to
// cycle through the rest".
package cycler

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"aerocycle/internal/aerospace"
	"aerocycle/internal/config"
)

// Cycler owns the per-app most-recently-used (MRU) state.
type Cycler struct {
	ac      *aerospace.Client
	timeout time.Duration

	mu      sync.Mutex
	known   map[int]aerospace.Window // window id -> info
	mru     map[string][]int         // bundle id -> window ids, most recent first
	focused int
	session session
	up      atomic.Bool // AeroSpace reachable (and enabled)
}

type session struct {
	bundle string
	order  []int // stable ordering for the duration of the session
	index  int
	last   time.Time
}

// New builds a Cycler.
func New(ac *aerospace.Client, cfg *config.Config) *Cycler {
	return &Cycler{
		ac:      ac,
		timeout: time.Duration(cfg.CycleTimeoutMS) * time.Millisecond,
		known:   map[int]aerospace.Window{},
		mru:     map[string][]int{},
	}
}

// Snapshot refreshes the window/MRU cache once and captures the currently
// focused window. Useful for one-shot invocations.
func (c *Cycler) Snapshot() {
	c.refresh()
	if cur, err := c.ac.Focused(); err == nil {
		c.mu.Lock()
		c.focused = cur.ID
		c.mu.Unlock()
	}
}

// Run keeps the window/MRU cache fresh: an initial snapshot, a periodic
// reconciliation (there is no "window closed" event in AeroSpace), and a live
// subscription to focus changes.
func (c *Cycler) Run(ctx context.Context) {
	c.Snapshot()
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				c.refresh()
			}
		}
	}()

	for {
		events, errs, err := c.ac.Subscribe(ctx)
		if err != nil {
			log.Printf("aerocycle: subscribe: %v (retrying)", err)
		} else {
			c.consume(ctx, events, errs)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

func (c *Cycler) consume(ctx context.Context, events <-chan aerospace.Event, errs <-chan error) {
	for {
		select {
		case <-ctx.Done():
			return
		case err := <-errs:
			if err != nil {
				log.Printf("aerocycle: event stream: %v", err)
			}
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			c.handleEvent(ev)
		}
	}
}

func (c *Cycler) handleEvent(ev aerospace.Event) {
	switch ev.Event {
	case "focus-changed":
		c.mu.Lock()
		c.focused = ev.WindowID
		w, ok := c.known[ev.WindowID]
		c.mu.Unlock()
		if ok {
			c.mu.Lock()
			c.mru[w.BundleID] = moveFront(c.mru[w.BundleID], ev.WindowID)
			c.mu.Unlock()
		}
	case "window-detected":
		c.mu.Lock()
		w := c.known[ev.WindowID]
		w.ID = ev.WindowID
		if ev.AppBundleID != "" {
			w.BundleID = ev.AppBundleID
		}
		if ev.AppName != "" {
			w.AppName = ev.AppName
		}
		if ev.Workspace != "" {
			w.Workspace = ev.Workspace
		}
		c.known[ev.WindowID] = w
		c.mu.Unlock()
	}
}

// Up reports whether AeroSpace is reachable. When it is not, the hotkey
// handler lets combos fall through instead of swallowing them.
func (c *Cycler) Up() bool { return c.up.Load() }

// SetTimeout updates the cycle timeout (used on config reload).
func (c *Cycler) SetTimeout(ms int) {
	if ms <= 0 {
		ms = 1200
	}
	c.mu.Lock()
	c.timeout = time.Duration(ms) * time.Millisecond
	c.mu.Unlock()
}

func (c *Cycler) setUp(v bool) {
	if c.up.Swap(v) != v {
		if v {
			log.Printf("aerocycle: connected to AeroSpace")
		} else {
			log.Printf("aerocycle: AeroSpace not reachable (shortcuts will pass through)")
		}
	}
}

// refresh rebuilds the window cache from a live listing.
func (c *Cycler) refresh() {
	wins, err := c.ac.ListAll()
	if err != nil {
		c.setUp(false)
		return
	}
	c.setUp(true)
	known := make(map[int]aerospace.Window, len(wins))
	byBundle := map[string][]int{}
	for _, w := range wins {
		known[w.ID] = w
		byBundle[w.BundleID] = append(byBundle[w.BundleID], w.ID)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.known = known
	for bundle, ids := range byBundle {
		c.mru[bundle] = reconcile(c.mru[bundle], ids)
	}
	for bundle := range c.mru {
		if _, ok := byBundle[bundle]; !ok {
			delete(c.mru, bundle)
		}
	}
}

// Cycle performs one press of an app's shortcut and returns the window it chose.
func (c *Cycler) Cycle(bundle string) (aerospace.Window, error) {
	wins, err := c.ac.ListForBundle(bundle)
	if err != nil {
		return aerospace.Window{}, err
	}
	if len(wins) == 0 {
		return aerospace.Window{}, fmt.Errorf("no managed windows for %s", bundle)
	}
	byID := make(map[int]aerospace.Window, len(wins))
	ids := make([]int, 0, len(wins))
	for _, w := range wins {
		byID[w.ID] = w
		ids = append(ids, w.ID)
	}

	c.mu.Lock()
	for id, w := range byID {
		c.known[id] = w
	}

	now := time.Now()
	fresh := c.session.bundle != bundle || now.Sub(c.session.last) > c.timeout

	// While a session is active we keep its own stable ordering and only
	// advance the index. Re-deriving the order from the MRU on every press
	// would move the target to the front and make the index skip windows.
	var order []int
	if fresh {
		order = reconcile(c.mru[bundle], ids)
		if len(order) == 0 {
			c.mu.Unlock()
			return aerospace.Window{}, fmt.Errorf("no windows for %s", bundle)
		}
		idx := 0
		if pos, ok := indexOf(order, c.focused); ok {
			// Already on one of this app's windows: go to the next one, so a
			// press always changes focus.
			idx = (pos + 1) % len(order)
		}
		c.session = session{bundle: bundle, order: order, index: idx, last: now}
	} else {
		order = reconcile(c.session.order, ids)
		if len(order) == 0 {
			c.mu.Unlock()
			return aerospace.Window{}, fmt.Errorf("no windows for %s", bundle)
		}
		c.session.order = order
		c.session.index = (c.session.index + 1) % len(order)
		c.session.last = now
	}

	target := byID[order[c.session.index]]
	c.mru[bundle] = moveFront(order, target.ID)
	c.focused = target.ID
	c.mu.Unlock()

	return target, c.focus(target)
}

func (c *Cycler) focus(w aerospace.Window) error {
	if cur, err := c.ac.Focused(); err == nil {
		if cur.ID == w.ID {
			return nil
		}
		if w.Workspace != "" && cur.Workspace != w.Workspace {
			// The window lives on another (possibly hidden) workspace; make it
			// visible before focusing it.
			if err := c.ac.Workspace(w.Workspace); err != nil {
				log.Printf("aerocycle: workspace %s: %v", w.Workspace, err)
			}
		}
	}
	return c.ac.FocusWindow(w.ID)
}

// reconcile keeps the existing MRU order, drops ids that no longer exist, and
// appends ids we have not seen before (in listing order).
func reconcile(mru, ids []int) []int {
	live := make(map[int]bool, len(ids))
	for _, id := range ids {
		live[id] = true
	}
	out := make([]int, 0, len(ids))
	seen := make(map[int]bool, len(ids))
	for _, id := range mru {
		if live[id] && !seen[id] {
			out = append(out, id)
			seen[id] = true
		}
	}
	for _, id := range ids {
		if !seen[id] {
			out = append(out, id)
			seen[id] = true
		}
	}
	return out
}

func indexOf(list []int, id int) (int, bool) {
	for i, x := range list {
		if x == id {
			return i, true
		}
	}
	return 0, false
}

func moveFront(list []int, id int) []int {
	out := make([]int, 0, len(list)+1)
	out = append(out, id)
	for _, x := range list {
		if x != id {
			out = append(out, x)
		}
	}
	return out
}
