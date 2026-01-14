// Package pagination provides pagination utilities for list views.
package pagination

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
)

// Pagination errors.
var (
	ErrInvalidPage    = errors.New("invalid page number")
	ErrInvalidPerPage = errors.New("invalid per page value")
)

// DefaultPerPage is the default number of items per page.
const DefaultPerPage = 20

// MaxPerPage is the maximum allowed items per page.
const MaxPerPage = 100

// Pagination holds pagination state and computed values.
type Pagination struct {
	Page       int    // Current page (1-based)
	PerPage    int    // Items per page
	Total      int    // Total number of items
	TotalPages int    // Total number of pages
	BaseURL    string // Base URL for pagination links
}

// New creates a new Pagination with computed values.
func New(page, perPage, total int, baseURL string) *Pagination {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = DefaultPerPage
	}
	if perPage > MaxPerPage {
		perPage = MaxPerPage
	}

	totalPages := int(math.Ceil(float64(total) / float64(perPage)))
	if totalPages < 1 {
		totalPages = 1
	}

	// Clamp page to valid range
	if page > totalPages {
		page = totalPages
	}

	return &Pagination{
		Page:       page,
		PerPage:    perPage,
		Total:      total,
		TotalPages: totalPages,
		BaseURL:    baseURL,
	}
}

// ParseFromQuery parses page and per_page from URL query parameters.
func ParseFromQuery(q url.Values) (page, perPage int) {
	pageStr := q.Get("page")
	if pageStr != "" {
		if parsed, err := strconv.Atoi(pageStr); err == nil && parsed > 0 {
			page = parsed
		}
	}
	if page < 1 {
		page = 1
	}

	perPageStr := q.Get("per_page")
	if perPageStr != "" {
		if parsed, err := strconv.Atoi(perPageStr); err == nil && parsed > 0 {
			perPage = parsed
		}
	}
	if perPage < 1 {
		perPage = DefaultPerPage
	}
	if perPage > MaxPerPage {
		perPage = MaxPerPage
	}

	return page, perPage
}

// HasPrevious returns true if there is a previous page.
func (p *Pagination) HasPrevious() bool {
	return p.Page > 1
}

// HasNext returns true if there is a next page.
func (p *Pagination) HasNext() bool {
	return p.Page < p.TotalPages
}

// PreviousPage returns the previous page number.
func (p *Pagination) PreviousPage() int {
	if p.HasPrevious() {
		return p.Page - 1
	}
	return 1
}

// NextPage returns the next page number.
func (p *Pagination) NextPage() int {
	if p.HasNext() {
		return p.Page + 1
	}
	return p.TotalPages
}

// Offset returns the offset for database queries.
func (p *Pagination) Offset() int {
	return (p.Page - 1) * p.PerPage
}

// Limit returns the limit for database queries.
func (p *Pagination) Limit() int {
	return p.PerPage
}

// PageURL returns the URL for a specific page.
func (p *Pagination) PageURL(page int) string {
	u, err := url.Parse(p.BaseURL)
	if err != nil {
		return fmt.Sprintf("?page=%d&per_page=%d", page, p.PerPage)
	}

	q := u.Query()
	q.Set("page", strconv.Itoa(page))
	q.Set("per_page", strconv.Itoa(p.PerPage))
	u.RawQuery = q.Encode()

	return u.String()
}

// PreviousURL returns the URL for the previous page.
func (p *Pagination) PreviousURL() string {
	return p.PageURL(p.PreviousPage())
}

// NextURL returns the URL for the next page.
func (p *Pagination) NextURL() string {
	return p.PageURL(p.NextPage())
}

// FirstURL returns the URL for the first page.
func (p *Pagination) FirstURL() string {
	return p.PageURL(1)
}

// LastURL returns the URL for the last page.
func (p *Pagination) LastURL() string {
	return p.PageURL(p.TotalPages)
}

// PageRange returns a range of page numbers for display.
// It returns up to maxPages pages centered around the current page.
func (p *Pagination) PageRange(maxPages int) []int {
	if maxPages < 1 {
		maxPages = 5
	}

	if p.TotalPages <= maxPages {
		pages := make([]int, p.TotalPages)
		for i := range pages {
			pages[i] = i + 1
		}
		return pages
	}

	// Calculate start and end with current page centered
	half := maxPages / 2
	start := p.Page - half
	end := p.Page + half

	// Adjust if we're near the beginning
	if start < 1 {
		start = 1
		end = maxPages
	}

	// Adjust if we're near the end
	if end > p.TotalPages {
		end = p.TotalPages
		start = end - maxPages + 1
	}

	pages := make([]int, end-start+1)
	for i := range pages {
		pages[i] = start + i
	}
	return pages
}

// StartItem returns the 1-based index of the first item on the current page.
func (p *Pagination) StartItem() int {
	if p.Total == 0 {
		return 0
	}
	return p.Offset() + 1
}

// EndItem returns the 1-based index of the last item on the current page.
func (p *Pagination) EndItem() int {
	end := p.Offset() + p.PerPage
	if end > p.Total {
		end = p.Total
	}
	return end
}

// IsEmpty returns true if there are no items.
func (p *Pagination) IsEmpty() bool {
	return p.Total == 0
}

// ShowPagination returns true if pagination controls should be shown.
func (p *Pagination) ShowPagination() bool {
	return p.TotalPages > 1
}
