package suwayomi

// GraphQL operations used by the module. Every operation here is validated
// against the pinned schema in testdata by TestOperationsMatchSchema.

const opAbout = `query About { aboutServer { name version revision buildType } }`

const opSources = `query Sources {
  sources { nodes { id name lang displayName supportsLatest contentWarning iconUrl extension { pkgName } } }
}`

const mangaFields = `id sourceId url title thumbnailUrl chaptersLastFetchedAt chapters { totalCount }`

const opFetchSourceManga = `mutation FetchSourceManga($source: LongString!, $type: FetchSourceMangaType!, $page: Int!, $query: String) {
  fetchSourceManga(input: {source: $source, type: $type, page: $page, query: $query}) {
    hasNextPage
    mangas { ` + mangaFields + ` }
  }
}`

const opFetchMangaAndChapters = `mutation FetchMangaAndChapters($id: Int!, $chapters: Boolean!) {
  fetchMangaAndChapters(input: {id: $id, fetchManga: true, fetchChapters: $chapters}) {
    manga { ` + mangaFields + ` author artist description genre status realUrl }
    chapters { id url name chapterNumber scanlator uploadDate sourceOrder realUrl }
  }
}`

const opFindManga = `query FindManga($sourceId: LongString!, $url: String!) {
  mangas(condition: {sourceId: $sourceId, url: $url}, first: 1) { nodes { id } }
}`

const opFindChapter = `query FindChapter($mangaId: Int!, $url: String!) {
  chapters(condition: {mangaId: $mangaId, url: $url}, first: 1) { nodes { id } }
}`

const opFetchChapterPages = `mutation FetchChapterPages($chapterId: Int!) {
  fetchChapterPages(input: {chapterId: $chapterId}) { pages chapter { id pageCount } }
}`

const extensionFields = `pkgName name lang versionName versionCodeLong isInstalled hasUpdate isObsolete contentWarning iconUrl`

const opExtensions = `query Extensions { extensions { nodes { ` + extensionFields + ` } } }`

const opFetchExtensions = `mutation FetchExtensions { fetchExtensions(input: {}) { extensions { ` + extensionFields + ` } } }`

const opUpdateExtension = `mutation UpdateExtension($id: String!, $install: Boolean, $update: Boolean, $uninstall: Boolean) {
  updateExtension(input: {id: $id, patch: {install: $install, update: $update, uninstall: $uninstall}}) {
    extension { pkgName isInstalled hasUpdate }
  }
}`

const opStores = `query Stores { extensionStores { nodes { indexUrl name } } }`

const opAddStore = `mutation AddStore($url: String!) { addExtensionStore(input: {indexUrl: $url}) { clientMutationId } }`

const opRemoveStore = `mutation RemoveStore($url: String!) { removeExtensionStore(input: {indexUrl: $url}) { clientMutationId } }`

const opPreferences = `query Preferences($id: LongString!) {
  source(id: $id) {
    preferences {
      __typename
      ... on SwitchPreference { key title summary visible switchValue: currentValue switchDefault: default }
      ... on CheckBoxPreference { key title summary visible checkValue: currentValue checkDefault: default }
      ... on EditTextPreference { key title summary visible textValue: currentValue textDefault: default }
      ... on ListPreference { key title summary visible listValue: currentValue listDefault: default entries entryValues }
      ... on MultiSelectListPreference { key title summary visible multiValue: currentValue multiDefault: default entries entryValues }
    }
  }
}`

const opUpdatePreference = `mutation UpdatePreference($source: LongString!, $change: SourcePreferenceChangeInput!) {
  updateSourcePreference(input: {source: $source, change: $change}) { clientMutationId }
}`

const opSetSettings = `mutation SetSettings($settings: PartialSettingsTypeInput!) {
  setSettings(input: {settings: $settings}) { settings { flareSolverrEnabled globalUpdateInterval } }
}`

const opClearCache = `mutation ClearCache { clearCachedImages(input: {cachedPages: true}) { cachedPages } }`

// allOps is used by the schema contract test.
var allOps = map[string]string{
	"About": opAbout, "Sources": opSources, "FetchSourceManga": opFetchSourceManga,
	"FetchMangaAndChapters": opFetchMangaAndChapters, "FindManga": opFindManga, "FindChapter": opFindChapter,
	"FetchChapterPages": opFetchChapterPages, "Extensions": opExtensions, "FetchExtensions": opFetchExtensions,
	"UpdateExtension": opUpdateExtension, "Stores": opStores, "AddStore": opAddStore, "RemoveStore": opRemoveStore,
	"Preferences": opPreferences, "UpdatePreference": opUpdatePreference, "SetSettings": opSetSettings,
	"ClearCache": opClearCache,
}
