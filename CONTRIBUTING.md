# Contributing

Build both binaries, run the tests:

```sh
go build -o ./bin/muster ./cmd/muster && go build -o ./bin/musterd ./cmd/musterd
go test ./... -race
```

Four things that otherwise cost an afternoon:

- **Rebuild before you look.** herdr runs what is in `bin/`, so an overlay
  change you have not built is one you will not see.
- **Restart the daemon after building `musterd`.** The running one is still the
  old one: `pkill -f 'musterd --daemon'`, then `./bin/musterd --ensure`.
- **Muster needs a state directory.** In a herdr pane it comes from
  `HERDR_PLUGIN_STATE_DIR`; from a plain shell, pass `--state-dir <dir>` and
  point it somewhere scratch, because `install` edits your real herdr config.
- **Do not start the herdr TUI from an agent session**, it hangs. Drive the
  server from the CLI: `herdr pane list`, `herdr pane read <pane-id>`,
  `musterd dump`.

CI runs build, vet and `test -race` on Linux and macOS.
