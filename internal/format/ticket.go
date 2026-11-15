package format

import (
	"fmt"
	stdhtml "html"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	botsdp "github.com/s2gamb/bot-go/internal/sdp"
	"github.com/s2gamb/sdp-go"
)

const (
	maxButtonLabelRunes = 48
	maxDetailDescRunes  = 2600
	telegramMaxRunes    = 4096
)

// FormatButtonLabel produces a concise single-line label for a ticket inline button.
func FormatButtonLabel(req sdp.DefaultRequest) string {
	subject := strings.TrimSpace(req.Subject)
	if subject == "" {
		subject = "(no subject)"
	}
	// Collapse any whitespace/newlines in subject
	subject = strings.Join(strings.Fields(subject), " ")

	emoji := statusEmoji(req.Status)
	prefix := fmt.Sprintf("#%s %s ", req.ID, emoji)
	if req.IsOverdue {
		prefix = fmt.Sprintf("🔥 #%s %s ", req.ID, emoji)
	}

	full := prefix + subject
	runeLen := utf8.RuneCountInString(full)
	if runeLen < maxButtonLabelRunes {
		return full + strings.Repeat(" ", maxButtonLabelRunes-runeLen-1) + "."
	}
	if runeLen == maxButtonLabelRunes {
		return full
	}

	runes := []rune(full)
	return string(runes[:maxButtonLabelRunes-1]) + "…"
}

// FormatListHeader builds the Telegram HTML message text for the ticket list screen.
func FormatListHeader(filterLabel string, page int, count int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "📋 <b>%s</b> — Page %d\n", stdhtml.EscapeString(filterLabel), page)
	if count == 0 {
		b.WriteString("\n<i>No tickets found matching this filter.</i>")
	} else {
		fmt.Fprintf(&b, "<i>Select a ticket below to view details (%d shown):</i>", count)
	}
	return b.String()
}

// FormatTicketDetail renders a single ticket's metadata and sanitized description in Telegram HTML.
func FormatTicketDetail(req *sdp.DefaultRequest, webURL string) string {
	if req == nil {
		return "<i>Ticket data unavailable.</i>"
	}

	subject := strings.TrimSpace(req.Subject)
	if subject == "" {
		subject = "(no subject)"
	}

	var meta strings.Builder
	if webURL != "" {
		fmt.Fprintf(&meta, "🎫 <a href=\"%s\"><b>#%s</b></a> — <b>%s</b>\n",
			stdhtml.EscapeString(webURL),
			stdhtml.EscapeString(req.ID),
			stdhtml.EscapeString(subject),
		)
	} else {
		fmt.Fprintf(&meta, "🎫 <b>#%s — %s</b>\n",
			stdhtml.EscapeString(req.ID),
			stdhtml.EscapeString(subject),
		)
	}

	meta.WriteString("\n")
	writeField(&meta, "Status", formatStatus(req))
	writeField(&meta, "Priority", formatRef(req.Priority, "—"))
	writeField(&meta, "Group", formatRef(req.Group, "Unassigned"))
	writeField(&meta, "Technician", formatUserRef(req.Technician, "Unassigned"))
	writeField(&meta, "Requester", formatUserRef(req.Requester, "—"))
	writeField(&meta, "Created", formatSDPTime(req.CreatedTime))
	if req.DueByTime != nil {
		writeField(&meta, "Due By", formatSDPTime(req.DueByTime))
	}

	meta.WriteString("\n<b>Description:</b>\n")

	var desc string
	if req.Description != nil && strings.TrimSpace(*req.Description) != "" {
		desc = SanitizeAndTruncateHTML(*req.Description, maxDetailDescRunes)
	} else if strings.TrimSpace(req.ShortDescription) != "" {
		desc = SanitizeAndTruncateHTML(req.ShortDescription, maxDetailDescRunes)
	}
	if desc == "" {
		desc = "<i>No description provided.</i>"
	}

	result := meta.String() + desc
	if utf8.RuneCountInString(result) > telegramMaxRunes {
		// Fallback safety net if metadata was unusually long
		budget := maxDetailDescRunes - (utf8.RuneCountInString(result) - telegramMaxRunes) - 100
		if budget < 200 {
			budget = 200
		}
		if req.Description != nil {
			desc = SanitizeAndTruncateHTML(*req.Description, budget)
		}
		result = meta.String() + desc
	}

	return result
}

func writeField(b *strings.Builder, label, value string) {
	if value == "" {
		return
	}
	fmt.Fprintf(b, "<b>%s:</b> %s\n", stdhtml.EscapeString(label), stdhtml.EscapeString(value))
}

func formatStatus(req *sdp.DefaultRequest) string {
	status := formatRef(getStatusRef(req.Status), "Unknown")
	if req.IsOverdue {
		return status + " (Overdue 🔥)"
	}
	return status
}

func statusEmoji(s *sdp.StatusRef) string {
	if s == nil {
		return "⚪"
	}
	name := strings.TrimSpace(s.Name)
	lower := strings.ToLower(name)
	switch {
	case strings.EqualFold(name, botsdp.StatusOpen), lower == "open", lower == "new":
		return "🟢"
	case strings.EqualFold(name, botsdp.StatusAssigned), lower == "assigned":
		return "🔵"
	case strings.EqualFold(name, botsdp.StatusInProgress), lower == "in progress", lower == "working":
		return "🔧"
	case strings.EqualFold(name, botsdp.StatusTesting), lower == "testing":
		return "🧪"
	case strings.EqualFold(name, botsdp.StatusScheduled), lower == "scheduled", lower == "planned":
		return "📅"
	case strings.EqualFold(name, botsdp.StatusOnHold), strings.Contains(lower, "hold"):
		return "🟡"
	case strings.EqualFold(name, botsdp.StatusWaitingInfo), strings.Contains(lower, "waiting"), strings.Contains(lower, "pending"):
		return "⏳"
	case strings.EqualFold(name, botsdp.StatusPendingApproval), strings.Contains(lower, "approval"):
		return "📝"
	case strings.EqualFold(name, botsdp.StatusDeveloperRequest), strings.Contains(lower, "developer"):
		return "💻"
	case strings.EqualFold(name, botsdp.StatusResolved), strings.EqualFold(name, botsdp.StatusCompleted), lower == "resolved", lower == "completed":
		return "✅"
	case strings.EqualFold(name, botsdp.StatusClosed), lower == "closed":
		return "⚫"
	case strings.EqualFold(name, botsdp.StatusCancelled), lower == "cancelled", lower == "canceled", lower == "rejected":
		return "❌"
	default:
		return "⚪"
	}
}

func getStatusRef(s *sdp.StatusRef) *sdp.Ref {
	if s == nil {
		return nil
	}
	return &s.Ref
}

func formatRef(r *sdp.Ref, fallback string) string {
	if r == nil || strings.TrimSpace(r.Name) == "" {
		return fallback
	}
	return strings.TrimSpace(r.Name)
}

func formatUserRef(u *sdp.UserRef, fallback string) string {
	if u == nil || strings.TrimSpace(u.Name) == "" {
		return fallback
	}
	name := strings.TrimSpace(u.Name)
	if u.IsVIPUser {
		name += " ⭐"
	}
	return name
}

func formatSDPTime(t *sdp.SDPTime) string {
	if t == nil {
		return "—"
	}
	if strings.TrimSpace(t.DisplayValue) != "" {
		return strings.TrimSpace(t.DisplayValue)
	}
	if ms, err := strconv.ParseInt(strings.TrimSpace(t.Value), 10, 64); err == nil && ms > 0 {
		return time.UnixMilli(ms).UTC().Format("2006-01-02 15:04 UTC")
	}
	return "—"
}
