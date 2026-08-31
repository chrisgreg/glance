# Contributing

Thanks for helping with Glance. Bug reports, fixes and small focused features
are all welcome.

## Scope

Glance is deliberately small: the useful numbers at a glance, one binary, one
SQLite file, no third-party services at runtime. Features that need external
databases, tracking cookies, session replay, funnels or a separate frontend
service are out of scope. Open an issue before starting anything large so we
can agree on the shape first.

## Setup

Requires Go and Node at the versions in `.tool-versions`.

```bash
cd server && GLANCE_DATABASE_PATH=./data/glance.db go run ./cmd/glance   # API on :8080
cd server/web && npm install && npm run dev                               # UI on :5173, proxies /api
```

## Checks

Run the full suite before opening a pull request. CI runs the same commands.

```bash
make test        # go vet, go test, svelte-check, vitest
make build       # single binary with the UI embedded
```

Add or update tests for behaviour changes. The Go tests drive the real HTTP
handlers with a fixed clock, so most changes can be covered end to end without
mocks.

## Pull requests

- One change per pull request, with a short description of what and why.
- Keep the README in step with user-facing changes.
- Add a line under `Unreleased` in `CHANGELOG.md`.
- Database changes go in a new numbered file under `server/migrations/`; never
  edit an existing migration.

## Releasing

Move the `Unreleased` entries in `CHANGELOG.md` under a new version heading,
commit, then tag:

```bash
git tag v1.2.3 && git push origin v1.2.3
```

The release workflow builds the Docker image for amd64 and arm64, pushes it to
`ghcr.io`, attaches binaries to a GitHub release, and uses the changelog
section as the release notes.
