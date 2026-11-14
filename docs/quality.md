# Quality checks

Every check below runs in CI and can be run locally. CI is the source of
truth; local runs use the same tools so a failure is reproducible before
pushing.

## Quick local run

```bash
direnv exec . bash -c '
  set -e
  gofmt -l .
  goimports -l .
  go vet ./...
  go mod tidy && git diff --exit-code -- go.mod go.sum
  golangci-lint run ./...
  govulncheck ./...
  gosec -quiet \
    -exclude=G304,G301,G302,G306,G204,G703 \
    -nosec-require-justification -nosec-require-rules ./...
  actionlint
  shellcheck nix/*.sh nix/tests/*.sh
  go test -race ./...
'
```

The Nix dev shell provides Go, `goimports`, `golangci-lint`,
`govulncheck`, `gosec`, `actionlint` and `shellcheck`.

## Checks and where they run

| Check | Catches | CI job |
| --- | --- | --- |
| `gofmt -l .`, `goimports -l .` | formatting drift, unsorted imports | `lint` (Format) |
| `golangci-lint run ./...` | style, dead code, common bugs | `lint` (golangci-lint) |
| `go vet ./...` | suspicious constructs the compiler accepts | `lint` (go vet) |
| `go mod tidy` + `git diff --exit-code` | stale `go.mod`/`go.sum` | `lint` (go mod tidy) |
| `actionlint` | invalid GitHub Actions workflows | `lint` (actionlint) |
| `shellcheck nix/*.sh nix/tests/*.sh` | shell bugs and quoting mistakes | `lint` (shellcheck) |
| `govulncheck ./...` | known vulnerabilities reachable from the code | `security` (govulncheck) |
| `gosec ./...` | unsafe Go patterns (overflow, subprocess, files) | `security` (gosec) |
| `go test -race ./...` | regressions and data races | `test` |
| dependency review | vulnerable or disallowed new dependencies | `Dependency Review` (pull requests only) |

The `Dependency Review` workflow uses `actions/dependency-review-action@v5`
and fails a pull request that adds a `high` or `critical` vulnerability. It
does not run on push, because it compares a pull request against its base.

## gosec exclusions

`security` runs gosec with a narrow, documented set of excluded rules:

```text
G304, G703  paths come from the user's own config or file picker; this is a
            local CLI, not a service that crosses a privilege boundary
G204        external programs (emulator, opener, sqlite3) are launched without
            a shell, so CWE-78 command injection does not apply
G301, G302, G306
            files and directories under the user's config/cache are ordinary
            0755/0644 user-owned data
```

Everything else stays enabled. In particular `G115` (integer overflow) is
active: the BPS/UPS footer parser compares sizes in `uint64` and clamps the
reported value, and VDF bit-pattern conversions go through documented
`asInt32`/`asUint32` helpers.

Do not add a global exclusion for a rule that fired for a real reason. Use a
targeted `// #nosec <rule> -- <reason>` comment instead; CI runs with
`-nosec-require-justification -nosec-require-rules`, so a suppression without
a rule id and a reason fails the build.

gosec is pinned to `v2.30.0` in CI so a new release cannot turn an unchanged
tree red. Bump it deliberately, then re-run the security job.

## Why some tools are run with `go run`

On `x86_64-darwin`, nixpkgs 26.05 lags the Go release cycle (Go 1.27.2 or
1.27.3 while `go.mod` requires the 1.27.4 toolchain). Tools built with an older
Go cannot analyse code that uses newer language features, so local runs use:

```bash
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
go run github.com/securego/gosec/v2/cmd/gosec@v2.30.0 ...
```

CI installs the same tools with the toolchain from `go.mod`, so it does not
need this workaround.

## Running inside the Nix dev shell

`nix develop --command sh -c '...'` can fail on macOS with a `syntax error
near unexpected token ';&'` from the nixpkgs setup script under the system
bash 3.2. Use `direnv exec . bash -c '...'` (or
`nix shell nixpkgs#bash -c bash -c '...'`) instead.
