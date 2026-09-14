# Writing modules

Everything mangarr talks to lives behind a module interface in
`internal/modules/<kind>`. Implementations live in
`internal/modules/<kind>/<impl>` and are the **only** packages allowed to
talk to vendor APIs (`scripts/import-lint.sh` enforces that core code never
imports an implementation).

## Anatomy

```go
package mymodule

type Settings struct {
    URL    string `json:"url" label:"Server URL" type:"url" required:"true" order:"1"`
    APIKey string `json:"apiKey" label:"API key" secret:"true" order:"2"`
    Extra  bool   `json:"extra" label:"Something advanced" advanced:"true" order:"3"`
}

func (s *Settings) Validate() error { return nil } // optional

func init() {
    modules.Register(&modules.Implementation{
        Kind: modules.KindMetadata, Name: "mymodule", DisplayName: "My provider",
        Description: "Shown when adding the module.",
        Settings: func() any { return &Settings{URL: "https://example.org"} }, // defaults
        New: func(deps modules.Deps, s any) (modules.Instance, error) {
            return &Module{s: s.(*Settings), http: deps.HTTP}, nil
        },
    })
}
```

Then add a blank import to `internal/modules/all/all.go`.

- **Field tags** (`label`, `help`, `type`, `order`, `required`, `advanced`,
  `secret`, `placeholder`, `options:"v:Label,…"`) become the schema served at
  `GET /api/v1/modules/schema`; the UI renders the form from it. Types:
  text, password, number, bool, select, url, textarea, tags (`[]string`),
  keyvalue (`map[string]string`).
- Secrets are masked in API responses and kept when the client sends the mask.
- `Test(ctx)` powers the **Test** button and health checks (unless the module
  implements `modules.HealthChecker` for a cheaper probe).
- Implement `modules.Closer` to release resources when settings change.

## Kinds

| Kind | Required interface | Optional capabilities |
|---|---|---|
| `source` | `source.Module`: `Sources`, `Search`, `Manga`, `Pages`, `FetchPage` | `Latest` (latest/popular), `ExtensionManager`, `Preferences`, `Thumbnails`, `Assets`, `Maintainer`, `AutoUpdater` |
| `metadata` | `metadata.Module`: `Search`, `Get` | `ExternalLookup` (join by another provider's id) |
| `library` | `library.Module`: `Rescan` | `ProgressReader` (per-reader credentials + progress) |
| `notify` | `notify.Module`: `Send` | — |
| `upscale` | `upscale.Module`: `Info`, `Upscale` | — |

### Source identity

A manga is identified by `(SourceID, URL)` — keep these **portable** (for
Tachiyomi sources: the Mihon source id and the path without domain) so
another engine can take over existing series via *reassign module*.
Engine-specific ids go into `EngineRef`, which the core caches but never
relies on. Report chapter numbers as the source gives them (`-1` when
unknown); the core parses names Mihon-style as a fallback.

### Metadata merging

Return provider-native ids in `ID` and cross ids in `ExternalIDs`
(`"anilist"`, `"mal"`, `"mangaupdates"`, …) so the aggregator can merge the
same series across providers. Scalars come from the highest-priority module
that has them; lists are unioned.

## Tests

- Contract tests use `httptest` servers (see `komga_test.go`,
  `kavita_test.go`, `notify_contract_test.go`).
- `internal/testutil/fakesource` and `fakelibrary` provide in-memory modules
  for pipeline tests (`internal/app/*_test.go`).
- Real-service tests go in `internal/integration` behind the `integration`
  build tag.
