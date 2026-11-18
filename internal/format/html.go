package format

import (
	stdhtml "html"
	"io"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
)

// SanitizeHTMLForTelegram converts SupportCenter HTML into safe, balanced Telegram HTML
// and replaces <img> elements with [Image] placeholders.
func SanitizeHTMLForTelegram(rawHTML string) string {
	return SanitizeAndTruncateHTML(rawHTML, 0)
}

// SanitizeAndTruncateHTML converts SupportCenter HTML into balanced Telegram HTML,
// replacing <img> tags with [Image], and truncating visible text to maxRunes (if maxRunes > 0)
// while closing any open HTML tags so Telegram ParseModeHTML never fails.
func SanitizeAndTruncateHTML(rawHTML string, maxRunes int) string {
	if strings.TrimSpace(rawHTML) == "" {
		return ""
	}

	tokenizer := html.NewTokenizer(strings.NewReader(rawHTML))
	var (
		out          strings.Builder
		openTags     []string
		skipDepth    int
		preDepth     int
		visibleRunes int
		truncated    bool
	)

	appendVisible := func(s string) bool {
		if s == "" {
			return true
		}
		if maxRunes > 0 {
			rCount := utf8.RuneCountInString(s)
			if visibleRunes+rCount > maxRunes {
				remaining := maxRunes - visibleRunes
				if remaining > 0 {
					runes := []rune(s)
					out.WriteString(stdhtml.EscapeString(string(runes[:remaining])))
					visibleRunes += remaining
				}
				truncated = true
				return false
			}
			visibleRunes += rCount
		}
		out.WriteString(stdhtml.EscapeString(s))
		return true
	}

	ensureTrailingNewline := func() {
		str := out.String()
		if str != "" && !strings.HasSuffix(str, "\n") {
			out.WriteByte('\n')
		}
	}

loop:
	for {
		tt := tokenizer.Next()
		switch tt {
		case html.ErrorToken:
			if tokenizer.Err() == io.EOF {
				break loop
			}
			break loop

		case html.TextToken:
			if skipDepth > 0 {
				continue
			}
			txt := string(tokenizer.Text())
			// Normalize CRLF to LF
			txt = strings.ReplaceAll(txt, "\r\n", "\n")
			txt = strings.ReplaceAll(txt, "\r", "\n")
			if preDepth == 0 && strings.TrimSpace(txt) == "" {
				if out.Len() == 0 || strings.HasSuffix(out.String(), "\n") {
					continue
				}
				if strings.Contains(txt, "\n") {
					txt = "\n"
				} else if strings.HasSuffix(out.String(), " ") {
					continue
				} else {
					txt = " "
				}
			}
			if !appendVisible(txt) {
				break loop
			}

		case html.StartTagToken, html.SelfClosingTagToken:
			tn, hasAttr := tokenizer.TagName()
			tag := strings.ToLower(string(tn))

			if skipDepth > 0 {
				if tt == html.StartTagToken && isSkippedContainer(tag) {
					skipDepth++
				}
				continue
			}

			if isSkippedContainer(tag) {
				if tt == html.StartTagToken {
					skipDepth = 1
				}
				continue
			}

			switch tag {
			case "img":
				if !appendVisible("[Image]") {
					break loop
				}
			case "br":
				if !appendVisible("\n") {
					break loop
				}
			case "p", "div", "tr", "h1", "h2", "h3", "h4", "h5", "h6", "blockquote":
				ensureTrailingNewline()
			case "li":
				ensureTrailingNewline()
				if !appendVisible("• ") {
					break loop
				}
			case "b", "strong":
				if tt == html.StartTagToken {
					out.WriteString("<b>")
					openTags = append(openTags, "b")
				}
			case "i", "em":
				if tt == html.StartTagToken {
					out.WriteString("<i>")
					openTags = append(openTags, "i")
				}
			case "u":
				if tt == html.StartTagToken {
					out.WriteString("<u>")
					openTags = append(openTags, "u")
				}
			case "s", "strike", "del":
				if tt == html.StartTagToken {
					out.WriteString("<s>")
					openTags = append(openTags, "s")
				}
			case "code":
				if tt == html.StartTagToken {
					out.WriteString("<code>")
					openTags = append(openTags, "code")
				}
			case "pre":
				if tt == html.StartTagToken {
					ensureTrailingNewline()
					out.WriteString("<pre>")
					openTags = append(openTags, "pre")
					preDepth++
				}
			case "a":
				if tt == html.StartTagToken {
					href := extractSafeHref(tokenizer, hasAttr)
					if href != "" {
						out.WriteString(`<a href="`)
						out.WriteString(stdhtml.EscapeString(href))
						out.WriteString(`">`)
						openTags = append(openTags, "a")
					}
				}
			}

		case html.EndTagToken:
			tn, _ := tokenizer.TagName()
			tag := strings.ToLower(string(tn))

			if skipDepth > 0 {
				if isSkippedContainer(tag) {
					skipDepth--
				}
				continue
			}

			switch tag {
			case "p", "div", "tr", "li", "h1", "h2", "h3", "h4", "h5", "h6", "blockquote":
				ensureTrailingNewline()
			case "b", "strong":
				openTags = closeMatchingTag(&out, openTags, "b")
			case "i", "em":
				openTags = closeMatchingTag(&out, openTags, "i")
			case "u":
				openTags = closeMatchingTag(&out, openTags, "u")
			case "s", "strike", "del":
				openTags = closeMatchingTag(&out, openTags, "s")
			case "code":
				openTags = closeMatchingTag(&out, openTags, "code")
			case "pre":
				openTags = closeMatchingTag(&out, openTags, "pre")
				if preDepth > 0 {
					preDepth--
				}
				ensureTrailingNewline()
			case "a":
				openTags = closeMatchingTag(&out, openTags, "a")
			}
		}
	}

	// Close any remaining open tags in reverse order.
	for i := len(openTags) - 1; i >= 0; i-- {
		out.WriteString("</")
		out.WriteString(openTags[i])
		out.WriteByte('>')
	}

	if truncated {
		out.WriteString("… <i>(truncated)</i>")
	}

	return strings.TrimSpace(cleanSanitizedHTML(out.String()))
}

func isSkippedContainer(tag string) bool {
	return tag == "script" || tag == "style" || tag == "head"
}

func extractSafeHref(z *html.Tokenizer, hasAttr bool) string {
	for hasAttr {
		var key, val []byte
		key, val, hasAttr = z.TagAttr()
		if strings.EqualFold(string(key), "href") {
			href := strings.TrimSpace(string(val))
			lower := strings.ToLower(href)
			if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "mailto:") {
				return href
			}
		}
	}
	return ""
}

func closeMatchingTag(out *strings.Builder, stack []string, target string) []string {
	idx := -1
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i] == target {
			idx = i
			break
		}
	}
	if idx == -1 {
		return stack
	}
	// Close any inner tags first, then target, then reopen inner tags if needed, or simply close down to idx.
	for i := len(stack) - 1; i >= idx; i-- {
		out.WriteString("</")
		out.WriteString(stack[i])
		out.WriteByte('>')
	}
	return stack[:idx]
}

func cleanSanitizedHTML(s string) string {
	if !strings.Contains(s, "<pre>") {
		return cleanTextBlock(s)
	}
	var b strings.Builder
	for {
		start := strings.Index(s, "<pre>")
		if start == -1 {
			b.WriteString(cleanTextBlock(s))
			break
		}
		b.WriteString(cleanTextBlock(s[:start]))
		rest := s[start:]
		end := strings.Index(rest, "</pre>")
		if end == -1 {
			b.WriteString(rest)
			break
		}
		end += len("</pre>")
		b.WriteString(rest[:end])
		s = rest[end:]
	}
	return b.String()
}

func cleanTextBlock(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.Trim(line, " \t\u00a0")
	}
	return collapseExcessNewlines(strings.Join(lines, "\n"))
}

func collapseExcessNewlines(s string) string {
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return s
}
