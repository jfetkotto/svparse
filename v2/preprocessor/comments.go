package preprocessor

import (
	"sort"
	"strings"
)

// CommentIndex answers position questions about a run's comments: what
// comment sits directly above a line (the doc comment of a declaration), and
// what comment trails a line of code. Build one from Result.Comments.
type CommentIndex struct {
	byFile map[string][]Comment // each file's comments, in position order, without duplicates
}

// NewCommentIndex indexes comments. A file included in several places is read
// once per inclusion and so contributes its comments each time; the index keeps
// one copy of each comment per position.
func NewCommentIndex(comments []Comment) *CommentIndex {
	idx := &CommentIndex{byFile: make(map[string][]Comment)}
	for _, c := range comments {
		idx.byFile[c.File] = append(idx.byFile[c.File], c)
	}
	for file, cs := range idx.byFile {
		sort.SliceStable(cs, func(i, j int) bool {
			if cs[i].Line != cs[j].Line {
				return cs[i].Line < cs[j].Line
			}
			return cs[i].Character < cs[j].Character
		})
		out := cs[:0]
		for i, c := range cs {
			if i > 0 && c.Line == cs[i-1].Line && c.Character == cs[i-1].Character {
				continue
			}
			out = append(out, c)
		}
		idx.byFile[file] = out
	}
	return idx
}

// All returns file's comments in position order.
func (x *CommentIndex) All(file string) []Comment { return x.byFile[file] }

// Leading returns the comments that document the code starting on line
// (zero-based): the unbroken run of own-line comments whose last line is the
// line just above, with no blank line between them or before the code. A
// blank line ends the run, so a comment separated from a declaration by one is
// not its documentation. Comments that trail a statement are never included.
// The result is in source order, nil if there is none.
func (x *CommentIndex) Leading(file string, line int) []Comment {
	cs := x.byFile[file]
	// Start at the last comment that ends before line, then walk upward while
	// each one touches the next.
	i := sort.Search(len(cs), func(i int) bool { return cs[i].Line >= line }) - 1
	var run []Comment
	want := line - 1 // the line the next comment up must end on
	for ; i >= 0; i-- {
		c := cs[i]
		endLine, _ := c.End()
		if !c.OwnLine || endLine != want {
			break
		}
		run = append(run, c)
		want = c.Line - 1
	}
	for l, r := 0, len(run)-1; l < r; l, r = l+1, r-1 {
		run[l], run[r] = run[r], run[l]
	}
	return run
}

// Trailing returns the comment that follows code on line (zero-based), as in
// "logic [7:0] x; // data bus".
func (x *CommentIndex) Trailing(file string, line int) (Comment, bool) {
	for _, c := range x.byFile[file] {
		if c.Line == line && !c.OwnLine {
			return c, true
		}
		if c.Line > line {
			break
		}
	}
	return Comment{}, false
}

// DocText joins comments' bodies into documentation text: each comment's Body,
// one per line, blank bodies kept so paragraphs survive.
func DocText(comments []Comment) string {
	parts := make([]string, len(comments))
	for i, c := range comments {
		parts[i] = c.Body()
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}
