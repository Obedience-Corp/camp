package vim

import (
	"crypto/sha256"

	"github.com/Obedience-Corp/camp/internal/ui/theme"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/charmbracelet/lipgloss"
)

// SyntaxStyler tokenizes content and provides per-offset styles for rendering.
// It caches results and only re-tokenizes when content changes.
type SyntaxStyler struct {
	styles   []lipgloss.Style
	tokens   []chroma.TokenType // parallel to styles, for testing
	hash     [32]byte           // SHA-256 of last tokenized content
	valid    bool
	lexer    chroma.Lexer
	tokenMap tokenStyleMap
}

// TokenAt returns the chroma token type at the given byte offset.
// Returns -1 for out-of-bounds offsets.
func (s *SyntaxStyler) TokenAt(offset int) chroma.TokenType {
	if offset < 0 || offset >= len(s.tokens) {
		return -1
	}
	return s.tokens[offset]
}

// tokenStyleMap holds the lipgloss styles for each chroma token type.
type tokenStyleMap struct {
	heading    lipgloss.Style
	bold       lipgloss.Style
	italic     lipgloss.Style
	code       lipgloss.Style
	link       lipgloss.Style
	keyword    lipgloss.Style
	deleted    lipgloss.Style
	defaultSty lipgloss.Style
}

// NewMarkdownStyler creates a SyntaxStyler configured for markdown.
func NewMarkdownStyler() *SyntaxStyler {
	pal := theme.TUI()
	return &SyntaxStyler{
		lexer: lexers.Get("markdown"),
		tokenMap: tokenStyleMap{
			heading:    lipgloss.NewStyle().Bold(true).Foreground(pal.Accent),
			bold:       lipgloss.NewStyle().Bold(true),
			italic:     lipgloss.NewStyle().Italic(true),
			code:       lipgloss.NewStyle().Foreground(pal.Success),
			link:       lipgloss.NewStyle().Foreground(pal.AccentAlt),
			keyword:    lipgloss.NewStyle().Foreground(pal.Warning),
			deleted:    lipgloss.NewStyle().Strikethrough(true).Foreground(pal.TextMuted),
			defaultSty: lipgloss.NewStyle(),
		},
	}
}

// Update re-tokenizes content if it changed since the last call.
func (s *SyntaxStyler) Update(content string, defaultStyle lipgloss.Style) {
	h := sha256.Sum256([]byte(content))
	if s.valid && h == s.hash {
		return
	}
	s.hash = h
	s.valid = true
	s.tokenMap.defaultSty = defaultStyle
	s.tokenize(content)
}

// StyleAt returns the style for the character at the given byte offset.
func (s *SyntaxStyler) StyleAt(offset int) lipgloss.Style {
	if offset < 0 || offset >= len(s.styles) {
		return s.tokenMap.defaultSty
	}
	return s.styles[offset]
}

// tokenize runs the chroma lexer and builds per-byte style and token slices.
func (s *SyntaxStyler) tokenize(content string) {
	n := len(content)
	s.styles = make([]lipgloss.Style, n)
	s.tokens = make([]chroma.TokenType, n)

	if s.lexer == nil {
		for i := range n {
			s.styles[i] = s.tokenMap.defaultSty
			s.tokens[i] = chroma.Text
		}
		return
	}

	iter, err := s.lexer.Tokenise(nil, content)
	if err != nil {
		for i := range n {
			s.styles[i] = s.tokenMap.defaultSty
			s.tokens[i] = chroma.Text
		}
		return
	}

	offset := 0
	for _, tok := range iter.Tokens() {
		style := s.styleForToken(tok.Type)
		for i := range len(tok.Value) {
			byteOffset := offset + i
			if byteOffset < n {
				s.styles[byteOffset] = style
				s.tokens[byteOffset] = tok.Type
			}
		}
		offset += len(tok.Value)
	}

	// Fill any remaining positions with default.
	for i := offset; i < n; i++ {
		s.styles[i] = s.tokenMap.defaultSty
		s.tokens[i] = chroma.Text
	}
}

// styleForToken maps a chroma token type to a lipgloss style.
func (s *SyntaxStyler) styleForToken(tt chroma.TokenType) lipgloss.Style {
	switch tt {
	case chroma.GenericHeading, chroma.GenericSubheading:
		return s.tokenMap.heading
	case chroma.GenericStrong:
		return s.tokenMap.bold
	case chroma.GenericEmph:
		return s.tokenMap.italic
	case chroma.LiteralStringBacktick, chroma.LiteralString:
		return s.tokenMap.code
	case chroma.NameTag, chroma.NameAttribute:
		return s.tokenMap.link
	case chroma.Keyword:
		return s.tokenMap.keyword
	case chroma.GenericDeleted:
		return s.tokenMap.deleted
	case chroma.NameEntity:
		return s.tokenMap.link
	default:
		return s.tokenMap.defaultSty
	}
}
