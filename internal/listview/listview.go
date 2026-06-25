package listview

import (
	"cmp"
	"slices"
	"strings"
)

type SortDirection string

const (
	Ascending  SortDirection = "asc"
	Descending SortDirection = "desc"
)

type SortKey struct {
	Name      string
	Value     string
	Rank      int
	Direction SortDirection
}

type Row struct {
	Key        string            `json:"key"`
	Title      string            `json:"title"`
	Subtitle   string            `json:"subtitle,omitempty"`
	Path       string            `json:"path,omitempty"`
	GroupKey   string            `json:"group_key,omitempty"`
	GroupLabel string            `json:"group_label,omitempty"`
	StyleToken string            `json:"style_token,omitempty"`
	SortKeys   []SortKey         `json:"-"`
	Fields     map[string]string `json:"fields,omitempty"`
}

type Section struct {
	Key        string `json:"key"`
	Label      string `json:"label"`
	StyleToken string `json:"style_token,omitempty"`
	Rows       []Row  `json:"rows"`
}

type Grouping struct {
	GroupBy          string   `json:"group_by,omitempty"`
	AvailableGroupBy []string `json:"available_group_by,omitempty"`
}

func Filter(rows []Row, groupFilter []string) []Row {
	if len(groupFilter) == 0 {
		return rows
	}
	allowed := make(map[string]bool, len(groupFilter))
	for _, group := range groupFilter {
		allowed[group] = true
	}
	out := make([]Row, 0, len(rows))
	for _, row := range rows {
		if allowed[row.GroupKey] {
			out = append(out, row)
		}
	}
	return out
}

func Sort(rows []Row) {
	slices.SortStableFunc(rows, CompareRows)
}

func CompareRows(a, b Row) int {
	maxLen := max(len(a.SortKeys), len(b.SortKeys))
	for i := 0; i < maxLen; i++ {
		var ak, bk SortKey
		if i < len(a.SortKeys) {
			ak = a.SortKeys[i]
		}
		if i < len(b.SortKeys) {
			bk = b.SortKeys[i]
		}
		if c := compareSortKey(ak, bk); c != 0 {
			return c
		}
	}
	if c := strings.Compare(a.Title, b.Title); c != 0 {
		return c
	}
	return strings.Compare(a.Key, b.Key)
}

func compareSortKey(a, b SortKey) int {
	c := cmp.Compare(a.Rank, b.Rank)
	if c == 0 {
		c = strings.Compare(a.Value, b.Value)
	}
	if a.Direction == Descending || b.Direction == Descending {
		return -c
	}
	return c
}

func Sections(rows []Row, groupBy string) []Section {
	if groupBy == "" {
		groupBy = "group"
	}
	var sections []Section
	index := map[string]int{}
	for _, row := range rows {
		key, label, styleToken := sectionValues(row, groupBy)
		if key == "" {
			key = "ungrouped"
			label = "Ungrouped"
		}
		i, ok := index[key]
		if !ok {
			i = len(sections)
			index[key] = i
			sections = append(sections, Section{
				Key:        key,
				Label:      label,
				StyleToken: styleToken,
				Rows:       []Row{},
			})
		}
		sections[i].Rows = append(sections[i].Rows, row)
	}
	return sections
}

func sectionValues(row Row, groupBy string) (key, label, styleToken string) {
	if groupBy == "group" {
		key = row.GroupKey
		label = row.GroupLabel
		styleToken = row.StyleToken
		if label == "" {
			label = key
		}
		return key, label, styleToken
	}
	if row.Fields != nil {
		key = row.Fields[groupBy]
	}
	label = key
	styleToken = groupBy + ":" + key
	return key, label, styleToken
}
