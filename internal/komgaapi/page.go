package komgaapi

import (
	"net/http"
	"strings"
)

// sortDTO is Spring's Sort.
type sortDTO struct {
	Sorted   bool `json:"sorted"`
	Unsorted bool `json:"unsorted"`
	Empty    bool `json:"empty"`
}

// pageableDTO is Spring's Pageable (KMReader requires every field).
type pageableDTO struct {
	Sort       sortDTO `json:"sort"`
	Offset     int     `json:"offset"`
	PageNumber int     `json:"pageNumber"`
	PageSize   int     `json:"pageSize"`
	Paged      bool    `json:"paged"`
	Unpaged    bool    `json:"unpaged"`
}

// pageDTO is Spring's Page JSON. The Mihon extension needs content, empty,
// first, last, number, numberOfElements, size, totalElements and totalPages;
// KMReader also needs pageable and sort.
type pageDTO[T any] struct {
	Content          []T         `json:"content"`
	Pageable         pageableDTO `json:"pageable"`
	TotalElements    int         `json:"totalElements"`
	TotalPages       int         `json:"totalPages"`
	Last             bool        `json:"last"`
	Size             int         `json:"size"`
	Number           int         `json:"number"`
	Sort             sortDTO     `json:"sort"`
	NumberOfElements int         `json:"numberOfElements"`
	First            bool        `json:"first"`
	Empty            bool        `json:"empty"`
}

// pageReq is a requested page (0-based). Unpaged returns everything as
// page 0, like Komga (size = max(count, 20)).
type pageReq struct {
	Page, Size int
	Unpaged    bool
	Sort       []string // e.g. "metadata.titleSort,asc"
}

func parsePage(r *http.Request, defSize int) pageReq {
	p := pageReq{Page: max(queryInt(r, "page", 0), 0), Size: queryInt(r, "size", defSize), Unpaged: queryBool(r, "unpaged")}
	if p.Size <= 0 {
		p.Size = defSize
	}
	p.Size = min(p.Size, 2000)
	for _, s := range r.URL.Query()["sort"] {
		if s = strings.TrimSpace(s); s != "" {
			p.Sort = append(p.Sort, s)
		}
	}
	return p
}

// paginate slices all into the requested page.
func paginate[T any](all []T, p pageReq) pageDTO[T] {
	total := len(all)
	size, number := p.Size, p.Page
	content := all
	if p.Unpaged {
		size, number = max(total, 20), 0
	} else {
		start := min(number*size, total)
		content = all[start:min(start+size, total)]
	}
	if content == nil {
		content = []T{}
	}
	pages := 0
	if size > 0 {
		pages = (total + size - 1) / size
	}
	sorted := len(p.Sort) > 0
	sd := sortDTO{Sorted: sorted, Unsorted: !sorted, Empty: !sorted}
	return pageDTO[T]{
		Content:          content,
		Pageable:         pageableDTO{Sort: sd, Offset: number * size, PageNumber: number, PageSize: size, Paged: true},
		TotalElements:    total,
		TotalPages:       pages,
		Last:             number >= pages-1,
		Size:             size,
		Number:           number,
		Sort:             sd,
		NumberOfElements: len(content),
		First:            number == 0,
		Empty:            len(content) == 0,
	}
}
