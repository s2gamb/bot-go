package sdp

import (
	"github.com/s2gamb/sdp-go"
)

// FilterKind identifies one of the ticket list filter presets.
type FilterKind string

const (
	FilterMyOpen     FilterKind = "my"
	FilterUnassigned FilterKind = "un"
	FilterMyGroups   FilterKind = "grp"
)

// AllFilters returns the ordered list of available filters in the Filter Picker dialog.
func AllFilters() []FilterKind {
	return []FilterKind{
		FilterMyOpen,
		FilterUnassigned,
		FilterMyGroups,
	}
}

// ParseFilterKind normalizes a raw filter code, defaulting to FilterMyOpen.
func ParseFilterKind(raw string) FilterKind {
	switch FilterKind(raw) {
	case FilterMyOpen, FilterUnassigned, FilterMyGroups:
		return FilterKind(raw)
	default:
		return FilterMyOpen
	}
}

// Label returns the human-readable title for the filter.
func (f FilterKind) Label() string {
	switch f {
	case FilterMyOpen:
		return "My Open Tickets"
	case FilterUnassigned:
		return "Unassigned Tickets"
	case FilterMyGroups:
		return "All My Groups"
	default:
		return "My Open Tickets"
	}
}

// BuildListOptions constructs the sdp.ListOptions for a given filter, technician ID, group IDs, and pagination state.
func BuildListOptions(filter FilterKind, techID string, groupIDs []string, page, perPage int) *sdp.ListOptions {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 8
	}

	opts := &sdp.ListOptions{
		Page:     page,
		RowCount: perPage,
		SortFields: map[string]string{
			"created_time": "desc",
		},
	}

	switch filter {
	case FilterUnassigned:
		criteria := []sdp.SearchCriterion{
			{
				Field:     "technician.id",
				Condition: "is",
				Value:     nil,
			},
		}
		criteria = append(criteria, openStatusCriteria(true)...)
		opts.SearchCriteria = criteria
	case FilterMyGroups:
		var criteria []sdp.SearchCriterion
		for i, gid := range groupIDs {
			c := sdp.SearchCriterion{
				Field:     "group.id",
				Condition: "is",
				Value:     gid,
			}
			if i > 0 {
				c.LogicalOperator = "or"
			}
			criteria = append(criteria, c)
		}
		criteria = append(criteria, openStatusCriteria(len(criteria) > 0)...)
		opts.SearchCriteria = criteria
	case FilterMyOpen:
		fallthrough
	default:
		var criteria []sdp.SearchCriterion
		if techID != "" {
			criteria = append(criteria, sdp.SearchCriterion{
				Field:     "technician.id",
				Condition: "is",
				Value:     techID,
			})
		}
		criteria = append(criteria, openStatusCriteria(len(criteria) > 0)...)
		opts.SearchCriteria = criteria
	}

	return opts
}

func openStatusCriteria(hasPreceding bool) []sdp.SearchCriterion {
	excluded := []string{StatusClosed, StatusCompleted, StatusCancelled}
	criteria := make([]sdp.SearchCriterion, 0, len(excluded))
	for i, st := range excluded {
		c := sdp.SearchCriterion{
			Field:     "status.name",
			Condition: "is not",
			Value:     st,
		}
		if hasPreceding || i > 0 {
			c.LogicalOperator = "and"
		}
		criteria = append(criteria, c)
	}
	return criteria
}
