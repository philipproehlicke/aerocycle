package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"aerocycle/internal/aerospace"
	"aerocycle/internal/config"
	"aerocycle/internal/cycler"
	"aerocycle/internal/hotkey"
	"aerocycle/internal/tui"
)

const version = "0.3.0"

func main() {
	// Subcommands must be handled before the flag package.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "tui", "config":
			if err := tui.Run(); err != nil {
				log.Fatalf("tui: %v", err)
			}
			return
		case "version", "-v", "--version":
			fmt.Println("aerocycle", version)
			return
		case "help", "-h", "--help":
			usage()
			return
		}
	}

	var (
		cfgPath    = flag.String("config", "", "path to config.toml (default ~/.config/aerocycle/config.toml)")
		debug      = flag.Bool("debug", false, "log detected right-\u2318 combos and cycling decisions")
		listApps   = flag.Bool("list-apps", false, "print bundle ids of running apps and exit")
		checkPerms = flag.Bool("check-permissions", false, "report Accessibility permission status and exit")
		reqPerm    = flag.Bool("request-permission", false, "ask macOS to show the Accessibility permission dialog, then exit")
		simulate   = flag.Bool("simulate", false, "read key names from stdin and cycle without the event tap (testing)")
	)
	flag.Parse()
	log.SetFlags(log.Ltime)

	switch {
	case *listApps:
		printRunningApps()
		return
	case *checkPerms:
		if hotkey.AccessibilityTrusted(false) {
			fmt.Println("Accessibility (this process): granted")
		} else {
			fmt.Println("Accessibility (this process): NOT granted")
			fmt.Println("Note: this reflects only the current process. The launchd daemon has its own grant.")
			fmt.Println("Check the daemon:  make agent-status   /   tail -n 5 /tmp/aerocycle.err.log")
		}
		return
	case *reqPerm:
		hotkey.AccessibilityTrusted(true)
		fmt.Println("If macOS showed a dialog, enable \"Aerocycle\" in:")
		fmt.Println("  System Settings > Privacy & Security > Accessibility")
		return
	case *simulate:
		runSimulate(*cfgPath)
		return
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v\n\nCreate one with `aerocycle tui` (or see config.example.toml)", err)
	}
	ac, err := aerospace.New()
	if err != nil {
		log.Fatalf("aerospace: %v", err)
	}

	cy := cycler.New(ac, cfg)

	// The active config can be swapped at runtime on SIGHUP.
	var current atomic.Pointer[config.Config]
	current.Store(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go cy.Run(ctx)

	writePid()
	defer removePid()

	// Reload on SIGHUP (sent by the TUI after saving, or manually).
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	go func() {
		for range hup {
			next, err := config.Load(*cfgPath)
			if err != nil {
				log.Printf("reload failed: %v", err)
				continue
			}
			current.Store(next)
			cy.SetTimeout(next.CycleTimeoutMS)
			log.Printf("config reloaded: %d shortcut(s) %v", len(next.Apps), appKeys(next))
		}
	}()

	hotkey.SetHandler(func(name string, isRepeat bool) bool {
		if !cy.Up() {
			// AeroSpace is down or disabled: let the combo reach the app.
			return false
		}
		cfg := current.Load()

		if bundle, ok := cfg.Apps[name]; ok {
			if *debug {
				log.Printf("right-\u2318+%s -> cycle %s (repeat=%v)", name, bundle, isRepeat)
			}
			if !isRepeat {
				go func() {
					if _, err := cy.Cycle(bundle); err != nil {
						log.Printf("cycle %s: %v", bundle, err)
					}
				}()
			}
			return true
		}

		if expr, ok := cfg.Actions[name]; ok {
			if *debug {
				log.Printf("right-\u2318+%s -> %s (repeat=%v)", name, expr, isRepeat)
			}
			if !isRepeat {
				go func() {
					if err := ac.Eval(expr); err != nil {
						log.Printf("action %q: %v", expr, err)
					}
				}()
			}
			return true
		}

		return false
	})

	waitForPermission()
	if err := hotkey.Install(); err != nil {
		log.Fatalf("hotkey: %v", err)
	}
	go hotkey.Run()

	log.Printf("aerocycle %s running: %d app(s) + %d action(s) on right-\u2318 %v",
		version, len(cfg.Apps), len(cfg.Actions), appKeys(cfg))

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	log.Printf("aerocycle stopping")
}

// waitForPermission asks for Accessibility once, then blocks until granted.
// Running under launchd (its own process) is what makes macOS attribute the
// grant to aerocycle rather than to a terminal.
func waitForPermission() {
	if hotkey.AccessibilityTrusted(true) {
		return
	}
	log.Printf("waiting for Accessibility permission")
	log.Printf("enable \"Aerocycle\" in System Settings > Privacy & Security > Accessibility")
	for !hotkey.AccessibilityTrusted(false) {
		time.Sleep(time.Second)
	}
	log.Printf("Accessibility permission granted")
}

func writePid() {
	if err := os.MkdirAll(config.Dir(), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(config.PidPath(), []byte(strconv.Itoa(os.Getpid())), 0o644)
}

func removePid() {
	_ = os.Remove(config.PidPath())
}

func usage() {
	fmt.Print(`aerocycle - right-Command window switching for AeroSpace

Usage:
  aerocycle            run the daemon
  aerocycle tui        edit shortcuts in a terminal UI
  aerocycle [flags]

Flags:
  -debug               log detected combos and cycling decisions
  -simulate            read key names from stdin and cycle (testing)
  -list-apps           print bundle ids of running apps
  -check-permissions   report Accessibility permission status
  -request-permission  trigger the macOS permission dialog
  -config <path>       use a different config file
`)
}

func appKeys(cfg *config.Config) []string {
	keys := make([]string, 0, len(cfg.Apps)+len(cfg.Actions))
	for k := range cfg.Apps {
		keys = append(keys, k)
	}
	for k := range cfg.Actions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func printRunningApps() {
	out, err := exec.Command("aerospace", "list-apps").Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to run `aerospace list-apps`: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(string(out))
}

// runSimulate exercises the cycle logic without the event tap: each non-empty
// line on stdin is treated as a right-\u2318 + <key> press.
func runSimulate(cfgPath string) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	ac, err := aerospace.New()
	if err != nil {
		log.Fatalf("aerospace: %v", err)
	}
	cy := cycler.New(ac, cfg)
	cy.Snapshot()

	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		key := strings.ToLower(strings.TrimSpace(sc.Text()))
		if key == "" {
			continue
		}
		bundle, ok := cfg.Apps[key]
		if ok {
			w, err := cy.Cycle(bundle)
			if err != nil {
				fmt.Printf("%s -> error: %v\n", key, err)
				continue
			}
			fmt.Printf("%s -> %d  %s  [ws %s]  %q\n", key, w.ID, w.AppName, w.Workspace, w.Title)
			continue
		}
		if expr, ok := cfg.Actions[key]; ok {
			if err := ac.Eval(expr); err != nil {
				fmt.Printf("%s -> action error: %v\n", key, err)
			} else {
				fmt.Printf("%s -> ran action: %s\n", key, expr)
			}
			continue
		}
		fmt.Printf("%s -> (no binding)\n", key)
	}
}
