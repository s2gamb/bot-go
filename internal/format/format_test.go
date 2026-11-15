package format

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/s2gamb/sdp-go"
)

func TestSanitizeHTMLForTelegram(t *testing.T) {
	raw := `
		<html><head><style>body { color: red; }</style></head>
		<body>
			<p>Hello <strong>World</strong> &amp; &lt;admin&gt;!</p>
			<div>Screenshot attached: <img src="/inline/1.png" alt="pic"/></div>
			<ul><li>First item</li><li>Second <a href="https://example.com/a&b">link</a></li></ul>
			<script>alert("xss")</script>
		</body></html>
	`

	got := SanitizeHTMLForTelegram(raw)

	if strings.Contains(got, "color: red") || strings.Contains(got, "alert(") {
		t.Fatalf("expected style and script content stripped, got: %s", got)
	}
	if !strings.Contains(got, "Hello <b>World</b> &amp; &lt;admin&gt;!") {
		t.Fatalf("expected bold and escaped entities, got: %s", got)
	}
	if !strings.Contains(got, "[Image]") {
		t.Fatalf("expected [Image] placeholder for <img>, got: %s", got)
	}
	if !strings.Contains(got, `• Second <a href="https://example.com/a&amp;b">link</a>`) {
		t.Fatalf("expected list bullet and safe link, got: %s", got)
	}
}

func TestSanitizeAndTruncateHTML_BalancedTags(t *testing.T) {
	raw := `<p><b><i>Very long bold italic text that will be truncated right in the middle</i></b></p>`
	got := SanitizeAndTruncateHTML(raw, 15)

	if !strings.HasSuffix(got, "</i></b>… <i>(truncated)</i>") {
		t.Fatalf("expected open tags closed before truncation suffix, got: %q", got)
	}
}

func TestFormatButtonLabel_And_TicketDetail(t *testing.T) {
	longDesc := `<p>` + strings.Repeat("Кириллица и ASCII текст с картинкой <img src='x'/> ", 120) + `</p>`
	req := &sdp.DefaultRequest{
		StandardRequest: sdp.StandardRequest{
			ID:          "606",
			Subject:     "Cannot connect to corporate VPN from home laptop when using Wi-Fi",
			Description: &longDesc,
			IsOverdue:   true,
			Status:      &sdp.StatusRef{Ref: sdp.Ref{Name: "Open"}},
			Priority:    &sdp.Ref{Name: "High"},
			Group:       &sdp.Ref{Name: "Network Team"},
			Technician:  &sdp.UserRef{Ref: sdp.Ref{Name: "Alice Tech"}},
			Requester:   &sdp.UserRef{Ref: sdp.Ref{Name: "Bob User"}, IsVIPUser: true},
			CreatedTime: &sdp.SDPTime{DisplayValue: "Nov 14, 2026 10:00 AM"},
		},
	}

	btn := FormatButtonLabel(*req)
	if utf8.RuneCountInString(btn) > maxButtonLabelRunes {
		t.Fatalf("button label exceeded max runes (%d): %q", utf8.RuneCountInString(btn), btn)
	}
	if !strings.HasPrefix(btn, "🔥 #606 🟢 ") {
		t.Fatalf("expected overdue + status prefix on button label, got: %q", btn)
	}

	detail := FormatTicketDetail(req, "https://sdp.local/WorkOrder.do?woMode=viewWO&woID=606")
	if utf8.RuneCountInString(detail) > telegramMaxRunes {
		t.Fatalf("detail message exceeded 4096 runes: %d", utf8.RuneCountInString(detail))
	}
	for _, want := range []string{
		`<a href="https://sdp.local/WorkOrder.do?woMode=viewWO&amp;woID=606"><b>#606</b></a>`,
		"Open (Overdue 🔥)",
		"Bob User ⭐",
		"[Image]",
		"(truncated)",
	} {
		if !strings.Contains(detail, want) {
			t.Errorf("detail output missing %q in:\n%s", want, detail)
		}
	}
}

func TestFormatButtonLabel_Padding(t *testing.T) {
	short := FormatButtonLabel(sdp.DefaultRequest{
		StandardRequest: sdp.StandardRequest{
			ID:      "42",
			Subject: "Short",
		},
	})
	if utf8.RuneCountInString(short) != maxButtonLabelRunes {
		t.Fatalf("expected padded length %d, got %d (%q)", maxButtonLabelRunes, utf8.RuneCountInString(short), short)
	}
	if !strings.HasPrefix(short, "#42 ⚪ Short ") || !strings.HasSuffix(short, " .") {
		t.Fatalf("expected trailing spaces and dot for short label, got %q", short)
	}

	// Exact length: "#42 ⚪ " is 6 runes, so 42 runes of subject = 48 runes total (no dot/padding)
	exactSubj := strings.Repeat("a", maxButtonLabelRunes-utf8.RuneCountInString("#42 ⚪ "))
	exact := FormatButtonLabel(sdp.DefaultRequest{
		StandardRequest: sdp.StandardRequest{
			ID:      "42",
			Subject: exactSubj,
		},
	})
	if utf8.RuneCountInString(exact) != maxButtonLabelRunes {
		t.Fatalf("expected exact length %d, got %d (%q)", maxButtonLabelRunes, utf8.RuneCountInString(exact), exact)
	}
	if exact != "#42 ⚪ "+exactSubj {
		t.Fatalf("expected unpadded exact label, got %q", exact)
	}
}

func TestFormatButtonLabel_StatusEmoji(t *testing.T) {
	cases := []struct {
		status string
		want   string
	}{
		{"Открыта", "#1 🟢 Test"},
		{"Назначена", "#1 🔵 Test"},
		{"В работе", "#1 🔧 Test"},
		{"Тестирование", "#1 🧪 Test"},
		{"Запланирована", "#1 📅 Test"},
		{"Приостановлена", "#1 🟡 Test"},
		{"Ожидание информации", "#1 ⏳ Test"},
		{"На согласовании", "#1 📝 Test"},
		{"Запрос разработчикам", "#1 💻 Test"},
		{"Закрыта", "#1 ⚫ Test"},
		{"Open", "#1 🟢 Test"},
		{"In Progress", "#1 🔧 Test"},
		{"On Hold", "#1 🟡 Test"},
		{"Waiting for customer", "#1 ⏳ Test"},
		{"Resolved", "#1 ✅ Test"},
		{"Closed", "#1 ⚫ Test"},
		{"Cancelled", "#1 ❌ Test"},
		{"Custom", "#1 ⚪ Test"},
	}

	for _, tc := range cases {
		got := FormatButtonLabel(sdp.DefaultRequest{
			StandardRequest: sdp.StandardRequest{
				ID:      "1",
				Subject: "Test",
				Status:  &sdp.StatusRef{Ref: sdp.Ref{Name: tc.status}},
			},
		})
		if !strings.HasPrefix(got, tc.want) {
			t.Errorf("status %q: expected prefix %q, got %q", tc.status, tc.want, got)
		}
	}
}
