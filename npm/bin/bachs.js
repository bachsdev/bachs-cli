#!/usr/bin/env node
/**
 * Launcher for the bachs CLI.
 *
 * The CLI itself is a Go binary. npm installs the one matching this platform
 * as an optional dependency, and this script hands control to it — so a Node
 * developer types `npm install -g @bachs/cli` and gets a native binary with no
 * Go toolchain and no Python involved.
 *
 * Optional dependencies are the mechanism: npm silently skips the ones whose
 * `os`/`cpu` do not match, so a Mac install never downloads the Windows build.
 */

const { spawnSync } = require("node:child_process");

const PLATFORMS = {
  "darwin-arm64": "@bachs/cli-darwin-arm64",
  "darwin-x64": "@bachs/cli-darwin-x64",
  "linux-arm64": "@bachs/cli-linux-arm64",
  "linux-x64": "@bachs/cli-linux-x64",
  "win32-x64": "@bachs/cli-win32-x64",
};

function resolveBinary() {
  const key = `${process.platform}-${process.arch}`;
  const pkg = PLATFORMS[key];

  if (!pkg) {
    throw new Error(
      `bachs does not ship a binary for ${key}.\n` +
        `Supported: ${Object.keys(PLATFORMS).join(", ")}\n` +
        `Install another way: https://docs.bachs.io/developer-portal/local-testing`
    );
  }

  const name = process.platform === "win32" ? "bachs.exe" : "bachs";
  try {
    return require.resolve(`${pkg}/bin/${name}`);
  } catch {
    // Reached when the optional dependency was skipped — usually
    // --no-optional, an offline install, or a locked-down registry.
    throw new Error(
      `The bachs binary for ${key} is not installed.\n` +
        `Reinstall without --no-optional:  npm install -g @bachs/cli\n` +
        `Or install directly:              brew install bachsdev/bachs/bachs`
    );
  }
}

let binary;
try {
  binary = resolveBinary();
} catch (err) {
  process.stderr.write(`error: ${err.message}\n`);
  process.exit(1);
}

// stdio: "inherit" so the terminal stays interactive — `bachs listen` streams
// output and must respond to Ctrl-C, neither of which survives piping.
const result = spawnSync(binary, process.argv.slice(2), { stdio: "inherit" });

if (result.error) {
  process.stderr.write(`error: could not run bachs: ${result.error.message}\n`);
  process.exit(1);
}

// Signals have no exit code; report them the way a shell does.
if (result.signal) {
  process.exit(1);
}
process.exit(result.status ?? 0);
