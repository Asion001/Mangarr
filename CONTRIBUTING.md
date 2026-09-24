# Contributing to mangarr

Thanks for helping. mangarr is still pre-1.0, so APIs, settings and the
database schema can change between versions.

## Before you start

- **Bugs and ideas** go in [GitHub issues](https://github.com/Asion001/Mangarr/issues).
  Search first; the backlog lives there.
- **Security problems** don't: see [SECURITY.md](SECURITY.md).
- For anything bigger than a small fix, open an issue first so we can agree
  on the approach before you spend time on it.

## Setting up

You need Go (the version in `go.mod`) and Node 22+ (or Docker, which
`make web` falls back to).

```bash
make web      # build the UI into web/dist
make build    # Go binary with the UI embedded
make run      # start it with ./config as the data dir
```

[docs/architecture.md](docs/architecture.md) explains how the pieces fit, and
[docs/modules.md](docs/modules.md) how to write a source, metadata, library
or notification module.

## Before you open a pull request

Run what CI runs:

```bash
scripts/gate.sh   # gofmt, vet, import rule, OpenAPI drift, Go tests
cd web && npm run lint && npm run typecheck && npm run check:i18n && npm test
```

- If you changed an API handler, regenerate the client types with
  `make web-types` and commit `web/openapi.json` and `web/src/api/schema.d.ts`.
- New UI text needs English, Russian and Ukrainian strings.
- Keep a pull request to one change, with tests for the behavior it adds or
  fixes.
- Commit messages follow [Conventional Commits](https://www.conventionalcommits.org)
  (`fix(reader): …`, `feat(sources): …`), as in the existing history.
- Don't put real personal data (emails, domains, usernames, your own
  library) in test fixtures or screenshots.

## Sources

Native site sources live in `internal/sources/sites`. Sites change often and
some block automated traffic; a fix for a broken source is always welcome.
Please don't add sources whose main purpose is bypassing paywalls of
official publishers.
