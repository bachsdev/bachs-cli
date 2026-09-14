package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/bachsdev/bachs-cli/internal/api"
	"github.com/bachsdev/bachs-cli/internal/config"
	"github.com/bachsdev/bachs-cli/internal/listen"
)

// deviceLogin runs the browser pairing flow.
//
// The point is that the key never reaches argv. Passing --api-key puts a
// credential in shell history and in /proc/<pid>/cmdline, where any local user
// can read it. Here the CLI holds a code that grants nothing until somebody
// approves it while signed in, and the key arrives over HTTPS.
//
// The key it receives is also weaker than one pasted by hand: it can read and
// it can drive local testing, and it cannot move money.
// environmentFor names the environment a base URL belongs to.
//
// Derived from the URL rather than passed alongside it, so it cannot drift out
// of step with the API actually being called. Anything not recognisably the
// sandbox is treated as live: guessing "sandbox" for an unknown host would
// send someone to look for a live code in the wrong place.
func environmentFor(baseURL string) string {
	if strings.Contains(baseURL, "sandbox") {
		return "sandbox"
	}
	return "live"
}

func deviceLogin(baseURL, deviceName string) int {
	ctx := context.Background()

	code, err := api.RequestDeviceCode(ctx, baseURL, deviceName)
	if err != nil {
		return fail(fmt.Errorf("could not start login: %w", err))
	}

	// Name the environment in the link. Sandbox and production keep separate
	// grant tables but share one approval page, so without this the page looks
	// the code up in whichever environment the dashboard happens to be toggled
	// to. A sandbox code viewed from a live dashboard is simply not found, and
	// reads to the developer as expired.
	//
	// Deliberately not `env`: the dashboard already owns that parameter for
	// sandbox deep links, and consumes and deletes it on arrival. Sharing the
	// name meant this one was stripped before the page could read it.
	url := fmt.Sprintf(
		"%s?code=%s&cli_env=%s",
		code.VerificationURI, code.UserCode, environmentFor(baseURL),
	)
	fmt.Printf(
		"\nYour pairing code is %s%s%s\n\n",
		listen.Bold, code.UserCode, listen.Reset,
	)
	fmt.Printf("Approve it at %s\n", url)

	if openBrowser(url) {
		fmt.Printf("%sOpened in your browser.%s\n", listen.Dim, listen.Reset)
	}
	fmt.Printf("\n%sWaiting for approval…%s ", listen.Dim, listen.Reset)

	interval := time.Duration(code.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(time.Duration(code.ExpiresIn) * time.Second)

	for {
		if time.Now().After(deadline) {
			fmt.Printf("\n%s✗%s the code expired. Run `bachs login` again.\n",
				listen.Red, listen.Reset)
			return 1
		}
		time.Sleep(interval)

		res, perr := api.PollDeviceToken(ctx, baseURL, code.DeviceCode)
		if perr != nil {
			// A poll failing is usually a blip, not a verdict. Keep waiting
			// until the code expires rather than abandoning a login the user
			// may have already approved.
			continue
		}

		switch res.Status {
		case "approved":
			if res.APIKey == "" {
				fmt.Printf("\n%s✗%s approved, but no key was returned. Try again.\n",
					listen.Red, listen.Reset)
				return 1
			}
			path, serr := config.Save(res.APIKey)
			if serr != nil {
				return fail(serr)
			}
			fmt.Printf("\n%s✓%s Logged in. Credentials saved to %s\n",
				listen.Green, listen.Reset, path)
			fmt.Printf(
				"%sThis key can read your data and run local testing. "+
					"It cannot move money, and it expires in 30 days.%s\n",
				listen.Dim, listen.Reset,
			)
			return 0

		case "denied":
			fmt.Printf("\n%s✗%s the request was denied.\n", listen.Red, listen.Reset)
			return 1

		case "expired":
			fmt.Printf("\n%s✗%s the code expired. Run `bachs login` again.\n",
				listen.Red, listen.Reset)
			return 1

		default:
			fmt.Print(".")
		}
	}
}

// openBrowser tries to open a URL, and reports whether it managed to.
//
// Best effort by design: the URL is printed either way, so a headless machine
// or an unusual desktop still gets a working flow rather than a dead end.
func openBrowser(url string) bool {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "windows":
		cmd, args = "rundll32", []string{"url.dll,FileProtocolHandler"}
	default:
		cmd = "xdg-open"
	}
	// A terminal with no display has nothing to open into, and guessing wrong
	// prints an error the user cannot act on.
	if runtime.GOOS == "linux" && os.Getenv("DISPLAY") == "" &&
		os.Getenv("WAYLAND_DISPLAY") == "" {
		return false
	}
	return exec.Command(cmd, append(args, url)...).Start() == nil
}
