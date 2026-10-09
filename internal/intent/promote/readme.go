package promote

import (
	"fmt"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// composeDesignReadme keeps authored Markdown in source order so workitem
// previews encounter the intent's opening text before generated provenance.
func composeDesignReadme(title, id, date, content string) string {
	body := cleanDesignBody(title, content)
	var out strings.Builder
	out.WriteString("# " + title + "\n\n")
	if body != "" {
		out.WriteString("## Content\n\n" + body)
		if closure := designFooterClosure(body); closure != "" {
			if !strings.HasSuffix(body, "\n") {
				out.WriteByte('\n')
			}
			out.WriteString(closure + "\n\n")
		} else {
			out.WriteString(paragraphBoundary(body))
		}
	}
	out.WriteString("## Status\n\n")
	fmt.Fprintf(&out, "In progress — promoted from intent %s on %s.\n", id, date)
	return out.String()
}

// Only these exact top-level template headings/comments are disposable. Parse
// Markdown to establish their context, but edit source spans, never reserialize
// the AST: HTML, code, lists, indentation and authored spacing must survive.
var designPlaceholders = map[string]string{
	"## context": "<!-- Why is this needed? What triggered this idea? -->",
	"## notes":   "<!-- Additional thoughts, references, or considerations -->",
}

type sourceSpan struct{ start, end int }

func cleanDesignBody(title, content string) string {
	doc := goldmark.DefaultParser().Parse(text.NewReader([]byte(content)))
	var removed []sourceSpan
	for node := doc.FirstChild(); node != nil; node = node.NextSibling() {
		heading, ok := node.(*ast.Heading)
		if !ok || heading.Lines().Len() == 0 {
			continue
		}
		line := sourceLine(content, heading.Lines().At(0).Start)
		label := strings.TrimSuffix(strings.TrimSuffix(content[line.start:line.end], "\n"), "\r")
		if node == doc.FirstChild() && heading.Level == 1 && label == "# "+title && markdownBlank(content[:line.start]) {
			removed = append(removed, sourceSpan{0, skipBlankLines(content, line.end)})
			continue
		}
		hint, known := designPlaceholders[strings.ToLower(label)]
		if heading.Level != 2 || !known {
			continue
		}

		end := len(content)
		var hints, edits []sourceSpan
		inSubsection := false
		for next := node.NextSibling(); next != nil; next = next.NextSibling() {
			if h, ok := next.(*ast.Heading); ok {
				if h.Level <= 2 && h.Lines().Len() > 0 {
					end = sourceLine(content, h.Lines().At(0).Start).start
					break
				}
				inSubsection = true
			}
			block, ok := next.(*ast.HTMLBlock)
			if inSubsection || !ok || block.HTMLBlockType != ast.HTMLBlockType2 || block.Lines().Len() != 1 || block.HasClosure() {
				continue
			}
			comment := sourceLine(content, block.Lines().At(0).Start)
			raw := strings.TrimSuffix(strings.TrimSuffix(content[comment.start:comment.end], "\n"), "\r")
			if raw == hint {
				// Keep the line ending so removing a hint cannot join lines.
				span := sourceSpan{comment.start - line.end, comment.start + len(raw) - line.end}
				hints = append(hints, span)
				before, after := next.PreviousSibling(), next.NextSibling()
				if before != nil && after != nil && before.Kind() != ast.KindHeading && after.Kind() != ast.KindHeading {
					// A blank line cannot separate every block (lists, quotes and
					// indented code can merge). Retain an empty HTML comment as
					// the original structural separator between authored blocks.
					span.start += len("<!--")
					span.end -= len("-->")
				}
				edits = append(edits, span)
			}
		}
		if markdownBlank(withoutSpans(content[line.end:end], hints)) {
			removed = append(removed, sourceSpan{line.start, end})
		} else {
			for _, edit := range edits {
				removed = append(removed, sourceSpan{line.end + edit.start, line.end + edit.end})
			}
		}
	}
	body := withoutSpans(content, removed)
	return body[skipBlankLines(body, 0):]
}

// withoutSpans accepts ordered, disjoint ranges. All bytes outside them are
// copied, including whitespace inside literal blocks and at the end of input.
func withoutSpans(source string, removed []sourceSpan) string {
	var out strings.Builder
	start := 0
	for _, span := range removed {
		out.WriteString(source[start:span.start])
		start = span.end
	}
	out.WriteString(source[start:])
	return out.String()
}

func sourceLine(source string, offset int) sourceSpan {
	start := strings.LastIndexByte(source[:offset], '\n') + 1
	end := len(source)
	if newline := strings.IndexByte(source[offset:], '\n'); newline >= 0 {
		end = offset + newline + 1
	}
	return sourceSpan{start, end}
}

// Unicode spaces (for example NBSP) can be authored paragraph content, not
// Markdown blank lines. Do not discard them with strings.TrimSpace.
func markdownBlank(source string) bool {
	return strings.Trim(source, " \t\r\n") == ""
}

func skipBlankLines(source string, start int) int {
	for start < len(source) {
		line := sourceLine(source, start)
		if !markdownBlank(source[start:line.end]) {
			break
		}
		start = line.end
	}
	return start
}

func paragraphBoundary(body string) string {
	if !strings.HasSuffix(body, "\n") {
		return "\n\n"
	}
	lastStart := strings.LastIndexByte(body[:len(body)-1], '\n') + 1
	if markdownBlank(body[lastStart:]) {
		return ""
	}
	return "\n"
}

// designFooterClosure checks how Markdown would parse a footer after this body.
// Only EOF-terminated top-level literal blocks can absorb it; container blocks
// (lists/quotes) end at the unindented footer. Let Goldmark decide rather than
// counting delimiters or treating code-like lines inside HTML as Markdown.
func designFooterClosure(body string) string {
	fences := &fenceDelimiterParser{
		BlockParser: parser.NewFencedCodeBlockParser(),
		delimiters:  make(map[ast.Node]string),
	}
	// Goldmark's default fenced-code parser has priority 700. This delegates
	// its grammar unchanged while retaining the opening delimiter it omits
	// from the AST, including for an empty, unterminated fence.
	md := goldmark.New(goldmark.WithParserOptions(parser.WithBlockParsers(util.Prioritized(fences, 699))))
	source := []byte(body + "\n\n## Status\n\nPromotion.\n")
	doc := md.Parser().Parse(text.NewReader(source))
	switch block := doc.LastChild().(type) {
	case *ast.FencedCodeBlock:
		return fences.delimiters[block]
	case *ast.HTMLBlock:
		switch block.HTMLBlockType {
		case ast.HTMLBlockType1:
			line := block.Lines().At(0)
			opener := strings.ToLower(strings.TrimLeft(string(line.Value(source)), " "))
			// Type 1 guarantees one of these names. Match the name alone;
			// HTML attributes may be separated by form feeds as well as spaces.
			for _, tag := range []string{"pre", "script", "style", "textarea"} {
				if strings.HasPrefix(opener, "<"+tag) {
					return "</" + tag + ">"
				}
			}
		case ast.HTMLBlockType2:
			return "-->"
		case ast.HTMLBlockType3:
			return "?>"
		case ast.HTMLBlockType4:
			return ">"
		case ast.HTMLBlockType5:
			return "]]>"
		}
	}
	return ""
}

type fenceDelimiterParser struct {
	parser.BlockParser
	delimiters map[ast.Node]string
}

func (p *fenceDelimiterParser) Open(parent ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	line, _ := reader.PeekLine()
	start := pc.BlockIndent()
	node, state := p.BlockParser.Open(parent, reader, pc)
	if node != nil {
		end := start
		for end < len(line) && line[end] == line[start] {
			end++
		}
		p.delimiters[node] = string(line[start:end])
	}
	return node, state
}
