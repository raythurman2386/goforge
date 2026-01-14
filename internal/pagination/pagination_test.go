package pagination

import (
	"net/url"
	"testing"
)

func TestNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		page           int
		perPage        int
		total          int
		wantPage       int
		wantPerPage    int
		wantTotalPages int
	}{
		{
			name:           "normal pagination",
			page:           1,
			perPage:        20,
			total:          100,
			wantPage:       1,
			wantPerPage:    20,
			wantTotalPages: 5,
		},
		{
			name:           "page less than 1 defaults to 1",
			page:           0,
			perPage:        20,
			total:          100,
			wantPage:       1,
			wantPerPage:    20,
			wantTotalPages: 5,
		},
		{
			name:           "negative page defaults to 1",
			page:           -5,
			perPage:        20,
			total:          100,
			wantPage:       1,
			wantPerPage:    20,
			wantTotalPages: 5,
		},
		{
			name:           "perPage less than 1 defaults to DefaultPerPage",
			page:           1,
			perPage:        0,
			total:          100,
			wantPage:       1,
			wantPerPage:    DefaultPerPage,
			wantTotalPages: 5,
		},
		{
			name:           "perPage exceeds MaxPerPage is capped",
			page:           1,
			perPage:        200,
			total:          100,
			wantPage:       1,
			wantPerPage:    MaxPerPage,
			wantTotalPages: 1,
		},
		{
			name:           "page beyond total pages is clamped",
			page:           100,
			perPage:        20,
			total:          50,
			wantPage:       3,
			wantPerPage:    20,
			wantTotalPages: 3,
		},
		{
			name:           "empty total results in 1 page",
			page:           1,
			perPage:        20,
			total:          0,
			wantPage:       1,
			wantPerPage:    20,
			wantTotalPages: 1,
		},
		{
			name:           "partial last page",
			page:           1,
			perPage:        20,
			total:          45,
			wantPage:       1,
			wantPerPage:    20,
			wantTotalPages: 3,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := New(tt.page, tt.perPage, tt.total, "")

			if p.Page != tt.wantPage {
				t.Errorf("Page = %d, want %d", p.Page, tt.wantPage)
			}
			if p.PerPage != tt.wantPerPage {
				t.Errorf("PerPage = %d, want %d", p.PerPage, tt.wantPerPage)
			}
			if p.TotalPages != tt.wantTotalPages {
				t.Errorf("TotalPages = %d, want %d", p.TotalPages, tt.wantTotalPages)
			}
		})
	}
}

func TestParseFromQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		query       string
		wantPage    int
		wantPerPage int
	}{
		{
			name:        "valid page and per_page",
			query:       "page=2&per_page=50",
			wantPage:    2,
			wantPerPage: 50,
		},
		{
			name:        "missing page defaults to 1",
			query:       "per_page=50",
			wantPage:    1,
			wantPerPage: 50,
		},
		{
			name:        "missing per_page defaults to DefaultPerPage",
			query:       "page=2",
			wantPage:    2,
			wantPerPage: DefaultPerPage,
		},
		{
			name:        "invalid page defaults to 1",
			query:       "page=abc&per_page=50",
			wantPage:    1,
			wantPerPage: 50,
		},
		{
			name:        "negative page defaults to 1",
			query:       "page=-5&per_page=50",
			wantPage:    1,
			wantPerPage: 50,
		},
		{
			name:        "per_page exceeds max is capped",
			query:       "page=1&per_page=500",
			wantPage:    1,
			wantPerPage: MaxPerPage,
		},
		{
			name:        "empty query uses defaults",
			query:       "",
			wantPage:    1,
			wantPerPage: DefaultPerPage,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			q, _ := url.ParseQuery(tt.query)
			page, perPage := ParseFromQuery(q)

			if page != tt.wantPage {
				t.Errorf("page = %d, want %d", page, tt.wantPage)
			}
			if perPage != tt.wantPerPage {
				t.Errorf("perPage = %d, want %d", perPage, tt.wantPerPage)
			}
		})
	}
}

func TestPagination_Navigation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		page         int
		total        int
		perPage      int
		hasPrevious  bool
		hasNext      bool
		previousPage int
		nextPage     int
	}{
		{
			name:         "first page",
			page:         1,
			total:        100,
			perPage:      20,
			hasPrevious:  false,
			hasNext:      true,
			previousPage: 1,
			nextPage:     2,
		},
		{
			name:         "middle page",
			page:         3,
			total:        100,
			perPage:      20,
			hasPrevious:  true,
			hasNext:      true,
			previousPage: 2,
			nextPage:     4,
		},
		{
			name:         "last page",
			page:         5,
			total:        100,
			perPage:      20,
			hasPrevious:  true,
			hasNext:      false,
			previousPage: 4,
			nextPage:     5,
		},
		{
			name:         "single page",
			page:         1,
			total:        10,
			perPage:      20,
			hasPrevious:  false,
			hasNext:      false,
			previousPage: 1,
			nextPage:     1,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := New(tt.page, tt.perPage, tt.total, "")

			if p.HasPrevious() != tt.hasPrevious {
				t.Errorf("HasPrevious() = %v, want %v", p.HasPrevious(), tt.hasPrevious)
			}
			if p.HasNext() != tt.hasNext {
				t.Errorf("HasNext() = %v, want %v", p.HasNext(), tt.hasNext)
			}
			if p.PreviousPage() != tt.previousPage {
				t.Errorf("PreviousPage() = %d, want %d", p.PreviousPage(), tt.previousPage)
			}
			if p.NextPage() != tt.nextPage {
				t.Errorf("NextPage() = %d, want %d", p.NextPage(), tt.nextPage)
			}
		})
	}
}

func TestPagination_OffsetLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		page       int
		perPage    int
		wantOffset int
		wantLimit  int
	}{
		{
			name:       "first page",
			page:       1,
			perPage:    20,
			wantOffset: 0,
			wantLimit:  20,
		},
		{
			name:       "second page",
			page:       2,
			perPage:    20,
			wantOffset: 20,
			wantLimit:  20,
		},
		{
			name:       "third page with custom per page",
			page:       3,
			perPage:    50,
			wantOffset: 100,
			wantLimit:  50,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := New(tt.page, tt.perPage, 1000, "")

			if p.Offset() != tt.wantOffset {
				t.Errorf("Offset() = %d, want %d", p.Offset(), tt.wantOffset)
			}
			if p.Limit() != tt.wantLimit {
				t.Errorf("Limit() = %d, want %d", p.Limit(), tt.wantLimit)
			}
		})
	}
}

func TestPagination_PageURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		baseURL  string
		page     int
		wantURL  string
		contains []string
	}{
		{
			name:     "simple base URL",
			baseURL:  "/items",
			page:     2,
			contains: []string{"page=2", "per_page=20"},
		},
		{
			name:     "base URL with existing query",
			baseURL:  "/items?filter=active",
			page:     3,
			contains: []string{"page=3", "per_page=20", "filter=active"},
		},
		{
			name:     "empty base URL",
			baseURL:  "",
			page:     1,
			contains: []string{"page=1", "per_page=20"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := New(1, 20, 100, tt.baseURL)
			url := p.PageURL(tt.page)

			for _, c := range tt.contains {
				if !containsSubstring(url, c) {
					t.Errorf("PageURL(%d) = %q, want to contain %q", tt.page, url, c)
				}
			}
		})
	}
}

func TestPagination_PageRange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		page      int
		total     int
		perPage   int
		maxPages  int
		wantPages []int
	}{
		{
			name:      "few pages shows all",
			page:      1,
			total:     60,
			perPage:   20,
			maxPages:  5,
			wantPages: []int{1, 2, 3},
		},
		{
			name:      "at beginning",
			page:      1,
			total:     200,
			perPage:   20,
			maxPages:  5,
			wantPages: []int{1, 2, 3, 4, 5},
		},
		{
			name:      "in middle",
			page:      5,
			total:     200,
			perPage:   20,
			maxPages:  5,
			wantPages: []int{3, 4, 5, 6, 7},
		},
		{
			name:      "at end",
			page:      10,
			total:     200,
			perPage:   20,
			maxPages:  5,
			wantPages: []int{6, 7, 8, 9, 10},
		},
		{
			name:      "single page",
			page:      1,
			total:     10,
			perPage:   20,
			maxPages:  5,
			wantPages: []int{1},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := New(tt.page, tt.perPage, tt.total, "")
			got := p.PageRange(tt.maxPages)

			if len(got) != len(tt.wantPages) {
				t.Errorf("PageRange(%d) = %v, want %v", tt.maxPages, got, tt.wantPages)
				return
			}

			for i, page := range got {
				if page != tt.wantPages[i] {
					t.Errorf("PageRange(%d)[%d] = %d, want %d", tt.maxPages, i, page, tt.wantPages[i])
				}
			}
		})
	}
}

func TestPagination_ItemRange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		page      int
		perPage   int
		total     int
		wantStart int
		wantEnd   int
	}{
		{
			name:      "first page full",
			page:      1,
			perPage:   20,
			total:     100,
			wantStart: 1,
			wantEnd:   20,
		},
		{
			name:      "second page",
			page:      2,
			perPage:   20,
			total:     100,
			wantStart: 21,
			wantEnd:   40,
		},
		{
			name:      "partial last page",
			page:      3,
			perPage:   20,
			total:     45,
			wantStart: 41,
			wantEnd:   45,
		},
		{
			name:      "empty results",
			page:      1,
			perPage:   20,
			total:     0,
			wantStart: 0,
			wantEnd:   0,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := New(tt.page, tt.perPage, tt.total, "")

			if p.StartItem() != tt.wantStart {
				t.Errorf("StartItem() = %d, want %d", p.StartItem(), tt.wantStart)
			}
			if p.EndItem() != tt.wantEnd {
				t.Errorf("EndItem() = %d, want %d", p.EndItem(), tt.wantEnd)
			}
		})
	}
}

func TestPagination_ShowPagination(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		total    int
		perPage  int
		wantShow bool
	}{
		{
			name:     "multiple pages shows pagination",
			total:    100,
			perPage:  20,
			wantShow: true,
		},
		{
			name:     "single page hides pagination",
			total:    10,
			perPage:  20,
			wantShow: false,
		},
		{
			name:     "exactly one page hides pagination",
			total:    20,
			perPage:  20,
			wantShow: false,
		},
		{
			name:     "empty results hides pagination",
			total:    0,
			perPage:  20,
			wantShow: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := New(1, tt.perPage, tt.total, "")

			if p.ShowPagination() != tt.wantShow {
				t.Errorf("ShowPagination() = %v, want %v", p.ShowPagination(), tt.wantShow)
			}
		})
	}
}

func TestPagination_IsEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		total   int
		isEmpty bool
	}{
		{
			name:    "has items",
			total:   100,
			isEmpty: false,
		},
		{
			name:    "no items",
			total:   0,
			isEmpty: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := New(1, 20, tt.total, "")

			if p.IsEmpty() != tt.isEmpty {
				t.Errorf("IsEmpty() = %v, want %v", p.IsEmpty(), tt.isEmpty)
			}
		})
	}
}

// containsSubstring checks if str contains substring.
func containsSubstring(str, substring string) bool {
	return len(str) >= len(substring) && (str == substring || len(str) > 0 && containsSubstringHelper(str, substring))
}

func containsSubstringHelper(str, substring string) bool {
	for i := 0; i <= len(str)-len(substring); i++ {
		if str[i:i+len(substring)] == substring {
			return true
		}
	}
	return false
}
