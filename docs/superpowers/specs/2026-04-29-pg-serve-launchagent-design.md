# AgentsView PG Serve LaunchAgent Design

## Goal

Run `agentsview pg serve` automatically for the current macOS user so the
AgentsView web UI can read from the configured PostgreSQL backend without
starting a shell session.

## Configuration

The LaunchAgent uses the existing `~/.agentsview/config.toml` for PostgreSQL
settings, including `[pg].url`, so database credentials stay out of the plist.
The job runs the installed AgentsView binary with:

```sh
/Users/mariusvniekerk/.local/share/mise/installs/github-wesm-agentsview/0.25.0/agentsview pg serve --no-browser --host 127.0.0.1 --port 18080
```

Port `18080` avoids the normal local SQLite-backed `agentsview serve` default
on `8080`.

## LaunchAgent

Install the plist at:

```text
/Users/mariusvniekerk/Library/LaunchAgents/io.agentsview.pg-serve.plist
```

The job label is `io.agentsview.pg-serve`. It uses `RunAtLoad`, keeps the
server alive after unexpected exits, throttles rapid restarts, and writes logs
to `~/.agentsview/pg-serve.launchd.{out,err}.log`.

## Verification

After bootstrapping the service into `gui/501`, verify:

```sh
launchctl print gui/501/io.agentsview.pg-serve
curl -fsS http://127.0.0.1:18080/api/v1/version
```

The version endpoint should return JSON with `"read_only": true`.
