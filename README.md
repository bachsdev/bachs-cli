# bachs CLI

Forward live webhook events to your local machine. No tunnel, no public URL, no
third-party account.

## Install

```bash
# macOS / Linux
brew install bachsdev/bachs/bachs

# Windows
scoop bucket add bachs https://github.com/bachsdev/scoop-bachs
scoop install bachs
```

Or grab a build for your platform from
[the releases page](https://github.com/bachsdev/bachs-cli/releases).

A single binary with no runtime dependencies — nothing to install first.

## Use

```bash
bachs login --api-key sk_sandbox_...
bachs listen --forward-to localhost:3000/webhooks
```

Every delivery prints as it arrives — event type, the status your handler
returned, and how long it took:

```
Ready! Forwarding sandbox events to http://localhost:3000/webhooks
Your webhook signing secret is whsec_a1b2c3... (^C to quit)
Session whls_8f2e… · 26 event type(s)

✓ collection.succeeded evt_3ab4e0d5 200 [42ms]
✗ refund.paid          evt_7c1f22a9 500 [131ms]
```

Narrow it down:

```bash
bachs listen --forward-to localhost:3000/webhooks --events collection.succeeded,refund.paid
```

Redeliver something that already happened. Needs no session and no socket — it
works against your registered endpoints too:

```bash
bachs events replay evt_3ab4e0d5d27445cf8a52ab3d8cb8f0b1
```

## How it works

The connection is opened **outbound** from your machine, so nothing needs to
reach you: no public URL, no inbound port, and it works behind NAT and
corporate firewalls.

Each session gets its **own signing secret**, printed when it starts. Payloads
are genuinely signed with it, so your verification code is exercised for real —
and your production endpoint's secret never leaves production.

A session is a webhook destination like any other, so event filtering, delivery
records and the dashboard all work the same way.

## Environments

The API key prefix picks the environment: `sk_sandbox_` for sandbox, `sk_live_`
for production. There is no `--env` flag to get out of step with the key you
are using.

## Notes

- Deliveries to a session are **not retried**. If your handler is down or your
  laptop is asleep, the event is shown as failed and dropped — you are watching
  the terminal, and a backlog arriving tomorrow would not help. Registered URL
  endpoints retry as normal.
- Credentials are stored at `~/.config/bachs/config.json`, mode 0600. Set
  `BACHS_API_KEY` instead if you would rather not write a key to disk.
- `NO_COLOR=1` disables colour; output is plain automatically when piped.

## Building from source

```bash
go build -o bachs ./cmd/bachs
```

Requires Go 1.22+. Releases are cut with
[GoReleaser](https://goreleaser.com) — see `.goreleaser.yml`.
