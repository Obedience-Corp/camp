package vim

import (
	"testing"

	"github.com/alecthomas/chroma/v2"
	"github.com/charmbracelet/lipgloss"
)

// hasTokenType checks if any byte offset in content has the given token type.
func hasTokenType(s *SyntaxStyler, content string, tt chroma.TokenType) bool {
	for i := range len(content) {
		if s.TokenAt(i) == tt {
			return true
		}
	}
	return false
}

func TestSyntaxStyler_EmptyContent(t *testing.T) {
	s := NewMarkdownStyler()
	s.Update("", lipgloss.NewStyle())

	if s.TokenAt(0) != -1 {
		t.Error("expected -1 token for out-of-bounds offset on empty content")
	}
}

func TestSyntaxStyler_PlainText(t *testing.T) {
	s := NewMarkdownStyler()
	s.Update("hello world", lipgloss.NewStyle())

	// All characters should be Text type.
	for i := range len("hello world") {
		if s.TokenAt(i) != chroma.Text {
			t.Errorf("offset %d: expected Text token, got %d", i, s.TokenAt(i))
		}
	}
}

func TestSyntaxStyler_HeadingStyle(t *testing.T) {
	s := NewMarkdownStyler()
	content := "# Hello\n"
	s.Update(content, lipgloss.NewStyle())

	if !hasTokenType(s, content, chroma.GenericHeading) {
		t.Error("expected GenericHeading token in '# Hello\\n'")
	}
}

func TestSyntaxStyler_SubheadingStyle(t *testing.T) {
	s := NewMarkdownStyler()
	content := "## Sub\n"
	s.Update(content, lipgloss.NewStyle())

	if !hasTokenType(s, content, chroma.GenericSubheading) {
		t.Error("expected GenericSubheading token in '## Sub\\n'")
	}
}

func TestSyntaxStyler_BoldStyle(t *testing.T) {
	s := NewMarkdownStyler()
	content := "text **bold** end\n"
	s.Update(content, lipgloss.NewStyle())

	if !hasTokenType(s, content, chroma.GenericStrong) {
		t.Error("expected GenericStrong token in 'text **bold** end'")
	}
}

func TestSyntaxStyler_ItalicStyle(t *testing.T) {
	s := NewMarkdownStyler()
	content := "text *italic* end\n"
	s.Update(content, lipgloss.NewStyle())

	if !hasTokenType(s, content, chroma.GenericEmph) {
		t.Error("expected GenericEmph token in 'text *italic* end'")
	}
}

func TestSyntaxStyler_InlineCodeStyle(t *testing.T) {
	s := NewMarkdownStyler()
	content := "use `code` here"
	s.Update(content, lipgloss.NewStyle())

	if !hasTokenType(s, content, chroma.LiteralStringBacktick) {
		t.Error("expected LiteralStringBacktick token for inline `code`")
	}
}

func TestSyntaxStyler_LinkStyle(t *testing.T) {
	s := NewMarkdownStyler()
	content := "see [link](http://example.com)\n"
	s.Update(content, lipgloss.NewStyle())

	if !hasTokenType(s, content, chroma.NameTag) {
		t.Error("expected NameTag token in '[link](url)'")
	}
	if !hasTokenType(s, content, chroma.NameAttribute) {
		t.Error("expected NameAttribute token in '[link](url)'")
	}
}

func TestSyntaxStyler_CacheHit(t *testing.T) {
	s := NewMarkdownStyler()
	content := "# Cached\n"

	s.Update(content, lipgloss.NewStyle())
	firstTokens := make([]chroma.TokenType, len(content))
	for i := range len(content) {
		firstTokens[i] = s.TokenAt(i)
	}

	// Update with same content — should be a no-op.
	s.Update(content, lipgloss.NewStyle())
	for i := range len(content) {
		if s.TokenAt(i) != firstTokens[i] {
			t.Fatalf("cache miss: token changed at offset %d", i)
		}
	}
}

func TestSyntaxStyler_ContentChange(t *testing.T) {
	s := NewMarkdownStyler()

	s.Update("plain text\n", lipgloss.NewStyle())
	plainToken := s.TokenAt(0)

	s.Update("# Heading\n", lipgloss.NewStyle())
	headingToken := s.TokenAt(0)

	if plainToken == headingToken {
		t.Error("expected different tokens after content change")
	}
}

func TestSyntaxStyler_OutOfBounds(t *testing.T) {
	s := NewMarkdownStyler()
	s.Update("hi", lipgloss.NewStyle())

	if s.TokenAt(-1) != -1 {
		t.Error("expected -1 for negative offset")
	}
	if s.TokenAt(100) != -1 {
		t.Error("expected -1 for offset beyond content")
	}
}

func TestSyntaxStyler_NilLexer(t *testing.T) {
	s := &SyntaxStyler{lexer: nil}
	s.Update("# test", lipgloss.NewStyle())

	// Should not panic; all tokens should be Text.
	for i := range len("# test") {
		if s.TokenAt(i) != chroma.Text {
			t.Errorf("nil lexer: expected Text at offset %d, got %d", i, s.TokenAt(i))
		}
	}
}
