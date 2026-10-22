package knowledgeops

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// Keep quoted attribute values together: a document title can contain > or
// backticks without ending its tag or opening a Markdown code span.
var adoptionCitationTag = regexp.MustCompile(`(?is)^<(?:kb|web)\b(?:[^"'<>]|"[^"]*"|'[^']*')*/?\s*>`)
var adoptionListMarker = regexp.MustCompile(`^ {0,3}(?:[-+*]|[0-9]{1,9}[.)])[ \t]+`)

// containsAdoptionCitation validates new publication content without rewriting
// the approved answer or the captured source. Code examples are literal text,
// so fenced blocks and matching backtick spans do not count as citations.
func containsAdoptionCitation(answer string) bool {
	var prose strings.Builder
	var fence byte
	var fenceSize int
	var container adoptionCodeContainer
	for _, line := range strings.SplitAfter(answer, "\n") {
		content, changed := container.content(line, fence != 0)
		if changed {
			if adoptionProseContainsCitation(prose.String()) {
				return true
			}
			prose.Reset()
			fence = 0
		}
		line = content
		marker, size, tail := adoptionFence(line)
		if fence != 0 {
			if marker == fence && size >= fenceSize && strings.TrimSpace(tail) == "" {
				fence = 0
			}
			continue
		}
		if size >= 3 && (marker != '`' || !strings.ContainsRune(tail, '`')) {
			if adoptionProseContainsCitation(prose.String()) {
				return true
			}
			prose.Reset()
			fence, fenceSize = marker, size
			continue
		}
		if strings.TrimSpace(line) == "" {
			// Inline code cannot span separate Markdown paragraphs.
			if adoptionProseContainsCitation(prose.String()) {
				return true
			}
			prose.Reset()
			continue
		}
		// Indented code starts a block; indentation on a continuing prose
		// paragraph alone does not turn an actual citation into an example.
		if prose.Len() == 0 && (strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t")) {
			continue
		}
		prose.WriteString(line)
	}
	return adoptionProseContainsCitation(prose.String())
}

// A code fence belongs to its enclosing quote/list item. Removing only that
// container's prefix exposes its code indentation while a dedent/quote exit
// closes its protection before any subsequent answer prose is checked.
type adoptionCodeContainer struct {
	parts []adoptionContainerPart
}

type adoptionContainerPart struct {
	quote  bool
	indent int
}

func (c *adoptionCodeContainer) content(line string, inFence bool) (string, bool) {
	consumed := 0
	for consumed < len(c.parts) {
		part := c.parts[consumed]
		if part.quote {
			prefix := adoptionQuotePrefix(line)
			if prefix == 0 {
				break
			}
			line = line[prefix:]
		} else if strings.TrimSpace(line) != "" {
			if !strings.HasPrefix(line, strings.Repeat(" ", part.indent)) {
				break
			}
			line = line[part.indent:]
		}
		consumed++
	}
	changed := consumed != len(c.parts)
	c.parts = c.parts[:consumed]
	// Prefix-like text inside an established code fence is literal. Only a
	// container exit permits parsing new prefixes on the same line.
	if inFence && !changed {
		return line, false
	}
	for {
		if prefix := adoptionQuotePrefix(line); prefix > 0 {
			c.parts = append(c.parts, adoptionContainerPart{quote: true})
			line = line[prefix:]
		} else if marker := adoptionListMarker.FindString(line); marker != "" {
			c.parts = append(c.parts, adoptionContainerPart{indent: len(marker)})
			line = line[len(marker):]
		} else {
			break
		}
		changed = true
	}
	return line, changed
}

func adoptionQuotePrefix(line string) int {
	indent := len(line) - len(strings.TrimLeft(line, " "))
	if indent > 3 || indent == len(line) || line[indent] != '>' {
		return 0
	}
	prefix := indent + 1
	if prefix < len(line) && (line[prefix] == ' ' || line[prefix] == '\t') {
		prefix++
	}
	return prefix
}

func adoptionFence(line string) (byte, int, string) {
	indent := len(line) - len(strings.TrimLeft(line, " "))
	if indent > 3 || indent == len(line) {
		return 0, 0, ""
	}
	line = line[indent:]
	marker := line[0]
	if marker != '`' && marker != '~' {
		return 0, 0, ""
	}
	size := 1
	for size < len(line) && line[size] == marker {
		size++
	}
	return marker, size, line[size:]
}

func adoptionProseContainsCitation(prose string) bool {
	for i := 0; i < len(prose); {
		switch prose[i] {
		case '\\':
			// Markdown escapes make the next delimiter literal.
			i += 2
			continue
		case '`':
			width := 1
			for i+width < len(prose) && prose[i+width] == '`' {
				width++
			}
			end := i + width
			for end < len(prose) {
				next := strings.IndexByte(prose[end:], '`')
				if next < 0 {
					break
				}
				end += next
				count := 1
				for end+count < len(prose) && prose[end+count] == '`' {
					count++
				}
				end += count
				if count == width {
					i = end
					break
				}
			}
			if i != end {
				// An unmatched opening run is ordinary Markdown text.
				i += width
			}
			continue
		case '<':
			if tag := adoptionCitationTag.FindString(prose[i:]); tag != "" {
				parser := html.NewTokenizer(strings.NewReader(tag))
				kind := parser.Next()
				if kind == html.SelfClosingTagToken || kind == html.StartTagToken {
					token := parser.Token()
					for _, attr := range token.Attr {
						if token.Data == "kb" && (attr.Key == "doc" || attr.Key == "chunk_id" || attr.Key == "chunkid" || attr.Key == "kb_id" || attr.Key == "kbid") || token.Data == "web" && attr.Key == "url" {
							return true
						}
					}
				}
				i += len(tag)
				continue
			}
		}
		i++
	}
	return false
}
