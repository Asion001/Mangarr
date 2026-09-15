package komgaapi

// DTOs follow Komga's (org.gotson.komga.interfaces.api.rest.dto) and carry
// the union of fields the clients decode as required:
//   - Mihon's Komga extension (Kotlin, ignoreUnknownKeys, non-null fields
//     without defaults are required)
//   - Mihon's Komga tracker (its own SeriesDto: the read counts)
//   - KMReader (Swift Decodable)
//   - Paperback 0.8 / 0.9 (TypeScript, reads what it needs)
// Slices are never nil so they encode as [].

type authorDTO struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

type webLinkDTO struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

type alternateTitleDTO struct {
	Label string `json:"label"`
	Title string `json:"title"`
}

// seriesMetadataDTO is SeriesMetadataDto. The Mihon extension requires the
// *Lock booleans, title, titleSort, summary, status, readingDirection,
// publisher, language, genres and tags.
type seriesMetadataDTO struct {
	Status               string              `json:"status"`
	StatusLock           bool                `json:"statusLock"`
	Title                string              `json:"title"`
	TitleLock            bool                `json:"titleLock"`
	TitleSort            string              `json:"titleSort"`
	TitleSortLock        bool                `json:"titleSortLock"`
	Summary              string              `json:"summary"`
	SummaryLock          bool                `json:"summaryLock"`
	ReadingDirection     string              `json:"readingDirection"`
	ReadingDirectionLock bool                `json:"readingDirectionLock"`
	Publisher            string              `json:"publisher"`
	PublisherLock        bool                `json:"publisherLock"`
	AgeRating            *int                `json:"ageRating"`
	AgeRatingLock        bool                `json:"ageRatingLock"`
	Language             string              `json:"language"`
	LanguageLock         bool                `json:"languageLock"`
	Genres               []string            `json:"genres"`
	GenresLock           bool                `json:"genresLock"`
	Tags                 []string            `json:"tags"`
	TagsLock             bool                `json:"tagsLock"`
	TotalBookCount       *int                `json:"totalBookCount"`
	TotalBookCountLock   bool                `json:"totalBookCountLock"`
	SharingLabels        []string            `json:"sharingLabels"`
	SharingLabelsLock    bool                `json:"sharingLabelsLock"`
	Links                []webLinkDTO        `json:"links"`
	LinksLock            bool                `json:"linksLock"`
	AlternateTitles      []alternateTitleDTO `json:"alternateTitles"`
	AlternateTitlesLock  bool                `json:"alternateTitlesLock"`
	Created              string              `json:"created"`
	LastModified         string              `json:"lastModified"`
}

// booksMetadataDTO is BookMetadataAggregationDto (Mihon requires summary,
// summaryNumber, created and lastModified; KMReader requires the object).
type booksMetadataDTO struct {
	Authors       []authorDTO `json:"authors"`
	Tags          []string    `json:"tags"`
	ReleaseDate   *string     `json:"releaseDate"`
	Summary       string      `json:"summary"`
	SummaryNumber string      `json:"summaryNumber"`
	Created       string      `json:"created"`
	LastModified  string      `json:"lastModified"`
}

// seriesDTO is SeriesDto.
type seriesDTO struct {
	ID                   string            `json:"id"`
	LibraryID            string            `json:"libraryId"`
	Name                 string            `json:"name"`
	URL                  string            `json:"url"`
	Created              string            `json:"created"`
	LastModified         string            `json:"lastModified"`
	FileLastModified     string            `json:"fileLastModified"`
	BooksCount           int               `json:"booksCount"`
	BooksReadCount       int               `json:"booksReadCount"`
	BooksUnreadCount     int               `json:"booksUnreadCount"`
	BooksInProgressCount int               `json:"booksInProgressCount"`
	Metadata             seriesMetadataDTO `json:"metadata"`
	BooksMetadata        booksMetadataDTO  `json:"booksMetadata"`
	Deleted              bool              `json:"deleted"`
	Oneshot              bool              `json:"oneshot"`
}

// mediaDTO is MediaDto. Every book is READY so undownloaded chapters show.
type mediaDTO struct {
	Status               string `json:"status"`
	MediaType            string `json:"mediaType"`
	MediaProfile         string `json:"mediaProfile"`
	PagesCount           int    `json:"pagesCount"`
	Comment              string `json:"comment"`
	EpubDivinaCompatible bool   `json:"epubDivinaCompatible"`
	EpubIsKepub          bool   `json:"epubIsKepub"`
}

// bookMetadataDTO is BookMetadataDto. The Mihon extension requires the
// *Lock booleans; its chapter number is numberSort and its date releaseDate.
type bookMetadataDTO struct {
	Title           string       `json:"title"`
	TitleLock       bool         `json:"titleLock"`
	Summary         string       `json:"summary"`
	SummaryLock     bool         `json:"summaryLock"`
	Number          string       `json:"number"`
	NumberLock      bool         `json:"numberLock"`
	NumberSort      float64      `json:"numberSort"`
	NumberSortLock  bool         `json:"numberSortLock"`
	ReleaseDate     *string      `json:"releaseDate"`
	ReleaseDateLock bool         `json:"releaseDateLock"`
	Authors         []authorDTO  `json:"authors"`
	AuthorsLock     bool         `json:"authorsLock"`
	Tags            []string     `json:"tags"`
	TagsLock        bool         `json:"tagsLock"`
	Isbn            string       `json:"isbn"`
	IsbnLock        bool         `json:"isbnLock"`
	Links           []webLinkDTO `json:"links"`
	LinksLock       bool         `json:"linksLock"`
	Created         string       `json:"created"`
	LastModified    string       `json:"lastModified"`
}

// readProgressDTO is ReadProgressDto (KMReader requires every field).
type readProgressDTO struct {
	Page         int    `json:"page"`
	Completed    bool   `json:"completed"`
	ReadDate     string `json:"readDate"`
	Created      string `json:"created"`
	LastModified string `json:"lastModified"`
	DeviceID     string `json:"deviceId"`
	DeviceName   string `json:"deviceName"`
}

// bookDTO is BookDto.
type bookDTO struct {
	ID               string           `json:"id"`
	SeriesID         string           `json:"seriesId"`
	SeriesTitle      string           `json:"seriesTitle"`
	LibraryID        string           `json:"libraryId"`
	Name             string           `json:"name"`
	URL              string           `json:"url"`
	Number           float64          `json:"number"`
	Created          string           `json:"created"`
	LastModified     string           `json:"lastModified"`
	FileLastModified string           `json:"fileLastModified"`
	SizeBytes        int64            `json:"sizeBytes"`
	Size             string           `json:"size"`
	Media            mediaDTO         `json:"media"`
	Metadata         bookMetadataDTO  `json:"metadata"`
	ReadProgress     *readProgressDTO `json:"readProgress"`
	Deleted          bool             `json:"deleted"`
	FileHash         string           `json:"fileHash"`
	Oneshot          bool             `json:"oneshot"`
}

// pageInfoDTO is PageDto (number is 1-based).
type pageInfoDTO struct {
	Number    int    `json:"number"`
	FileName  string `json:"fileName"`
	MediaType string `json:"mediaType"`
	Width     *int   `json:"width"`
	Height    *int   `json:"height"`
	SizeBytes *int64 `json:"sizeBytes"`
	Size      string `json:"size"`
}

// collectionDTO is CollectionDto (mangarr tags).
type collectionDTO struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Ordered          bool     `json:"ordered"`
	SeriesIDs        []string `json:"seriesIds"`
	CreatedDate      string   `json:"createdDate"`
	LastModifiedDate string   `json:"lastModifiedDate"`
	Filtered         bool     `json:"filtered"`
}

// readListDTO is ReadListDto ("Continue reading").
type readListDTO struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Summary          string   `json:"summary"`
	Ordered          bool     `json:"ordered"`
	BookIDs          []string `json:"bookIds"`
	CreatedDate      string   `json:"createdDate"`
	LastModifiedDate string   `json:"lastModifiedDate"`
	Filtered         bool     `json:"filtered"`
}

// tachiyomiProgressDTO is TachiyomiReadProgressV2Dto (Mihon's Komga tracker
// and Paperback 0.9).
type tachiyomiProgressDTO struct {
	BooksCount                   int     `json:"booksCount"`
	BooksReadCount               int     `json:"booksReadCount"`
	BooksUnreadCount             int     `json:"booksUnreadCount"`
	BooksInProgressCount         int     `json:"booksInProgressCount"`
	LastReadContinuousNumberSort float64 `json:"lastReadContinuousNumberSort"`
	MaxNumberSort                float64 `json:"maxNumberSort"`
}
