package bot

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/s2gamb/bot-go/internal/sdp"
)

const (
	CallbackNoop         = "noop"
	PrefixList           = "l:"
	PrefixFilterPicker   = "fp:"
	PrefixDetail         = "d:"
	PrefixDetailAction   = "a:"
	maxCallbackDataBytes = 64
)

// EncodeListCallback builds callback_data for displaying a paginated request list.
func EncodeListCallback(filter sdp.FilterKind, page int) string {
	if page < 1 {
		page = 1
	}
	return truncateCallback(fmt.Sprintf("%s%s:%d", PrefixList, sdp.ParseFilterKind(string(filter)), page))
}

// DecodeListCallback parses "l:<filter>:<page>".
func DecodeListCallback(data string) (sdp.FilterKind, int, bool) {
	if !strings.HasPrefix(data, PrefixList) {
		return "", 0, false
	}
	parts := strings.Split(strings.TrimPrefix(data, PrefixList), ":")
	if len(parts) != 2 {
		return "", 0, false
	}
	page, err := strconv.Atoi(parts[1])
	if err != nil || page < 1 {
		return "", 0, false
	}
	return sdp.ParseFilterKind(parts[0]), page, true
}

// EncodeFilterPickerCallback builds callback_data for opening the inline Filter Picker dialog.
func EncodeFilterPickerCallback(currentFilter sdp.FilterKind, page int) string {
	if page < 1 {
		page = 1
	}
	return truncateCallback(fmt.Sprintf("%s%s:%d", PrefixFilterPicker, sdp.ParseFilterKind(string(currentFilter)), page))
}

// DecodeFilterPickerCallback parses "fp:<filter>:<page>".
func DecodeFilterPickerCallback(data string) (sdp.FilterKind, int, bool) {
	if !strings.HasPrefix(data, PrefixFilterPicker) {
		return "", 0, false
	}
	parts := strings.Split(strings.TrimPrefix(data, PrefixFilterPicker), ":")
	if len(parts) != 2 {
		return "", 0, false
	}
	page, err := strconv.Atoi(parts[1])
	if err != nil || page < 1 {
		return "", 0, false
	}
	return sdp.ParseFilterKind(parts[0]), page, true
}

// EncodeDetailCallback builds callback_data for opening a ticket detail view.
func EncodeDetailCallback(ticketID string, filter sdp.FilterKind, page int) string {
	if page < 1 {
		page = 1
	}
	return truncateCallback(fmt.Sprintf("%s%s:%s:%d", PrefixDetail, ticketID, sdp.ParseFilterKind(string(filter)), page))
}

// DecodeDetailCallback parses "d:<ticketID>:<filter>:<page>".
func DecodeDetailCallback(data string) (string, sdp.FilterKind, int, bool) {
	if !strings.HasPrefix(data, PrefixDetail) {
		return "", "", 0, false
	}
	parts := strings.Split(strings.TrimPrefix(data, PrefixDetail), ":")
	if len(parts) != 3 || parts[0] == "" {
		return "", "", 0, false
	}
	page, err := strconv.Atoi(parts[2])
	if err != nil || page < 1 {
		return "", "", 0, false
	}
	return parts[0], sdp.ParseFilterKind(parts[1]), page, true
}

// EncodeActionCallback builds callback_data for a pluggable v2 detail sub-dialog action.
func EncodeActionCallback(actionID, ticketID string, filter sdp.FilterKind, page int) string {
	if page < 1 {
		page = 1
	}
	return truncateCallback(fmt.Sprintf("%s%s:%s:%s:%d", PrefixDetailAction, actionID, ticketID, sdp.ParseFilterKind(string(filter)), page))
}

// DecodeActionCallback parses "a:<actionID>:<ticketID>:<filter>:<page>".
func DecodeActionCallback(data string) (string, string, sdp.FilterKind, int, bool) {
	if !strings.HasPrefix(data, PrefixDetailAction) {
		return "", "", "", 0, false
	}
	parts := strings.Split(strings.TrimPrefix(data, PrefixDetailAction), ":")
	if len(parts) != 4 || parts[0] == "" || parts[1] == "" {
		return "", "", "", 0, false
	}
	page, err := strconv.Atoi(parts[3])
	if err != nil || page < 1 {
		return "", "", "", 0, false
	}
	return parts[0], parts[1], sdp.ParseFilterKind(parts[2]), page, true
}

func truncateCallback(s string) string {
	if len(s) <= maxCallbackDataBytes {
		return s
	}
	return s[:maxCallbackDataBytes]
}
