package main

import (
	"regexp"
	"strings"
)

// re matches text to produce token.
type rule struct {
	re    *regexp.Regexp
	tokid int
}

// Pre-compile regexps for token matching.
func newRule(pattern string, tokid int) *rule {
	return &rule{regexp.MustCompile("^" + pattern), tokid}
}

var rules = []*rule{
	// Order matters.
	newRule("\\s+", SKIP),   // whitespace
	newRule("#.*\\n", SKIP), // Python-style comments
	newRule("=", EQUALS),
	newRule("\\(", LPAREN),
	newRule("\\)", RPAREN),
	newRule("{", LBRACE),
	newRule("}", RBRACE),
	newRule("\\[", LBRACKET),
	newRule("\\]", RBRACKET),
	newRule(";", SEMICOLON),
	newRule(",", COMMA),
	newRule("\\.", DOT),
	newRule("\"[^\\\"]*\"", LITSTRING), // double-quoted strings. escapes not supported
	newRule("filetype\\b", FILETYPE),
	newRule("stage\\b", STAGE),
	newRule("pipeline\\b", PIPELINE),
	newRule("call\\b", CALL),
	newRule("volatile\\b", VOLATILE),
	newRule("sweep\\b", SWEEP),
	newRule("split\\b", SPLIT),
	newRule("using\\b", USING),
	newRule("self\\b", SELF),
	newRule("return\\b", RETURN),
	newRule("in\\b", IN),
	newRule("out\\b", OUT),
	newRule("src\\b", SRC),
	newRule("py\\b", PY),
	newRule("go\\b", GO),
	newRule("sh\\b", SH),
	newRule("exec\\b", EXEC),
	newRule("int\\b", INT),
	newRule("string\\b", STRING),
	newRule("float\\b", FLOAT),
	newRule("path\\b", PATH),
	newRule("file\\b", FILE),
	newRule("bool\\b", BOOL),
	newRule("true\\b", TRUE),
	newRule("false\\b", FALSE),
	newRule("null\\b", NULL),
	newRule("default\\b", DEFAULT),
	newRule("[a-zA-Z_][a-zA-z0-9_]*", ID),
	newRule("-?[0-9]+\\.[0-9]+([eE][-+]?[0-9]+)?\\b", NUM_FLOAT), // support exponential
	newRule("-?[0-9]+\\b", NUM_INT),
	newRule(".", INVALID),
}

type mmLex struct {
	source string   // All the data we're scanning
	pos    int      // Position of the scan head
	loc    int      // Keep track of the line number
	token  string   // Cache the last token for error messaging
}

func (self *mmLex) Lex(lval *mmSymType) int {
	// Loop until we return a token or run out of data.
	for {
		// Stop if we run out of data.
		if self.pos >= len(self.source) {
			return 0
		}
		// Slice the data using pos as a cursor.
		head := self.source[self.pos:]

		// Iterate through the regexps until one matches the head.
		var val string
		var r *rule
		for _, r = range rules {
			val = r.re.FindString(head)
			if len(val) > 0 {
				break
			}
		}

		// Advance the cursor pos.
		self.pos += len(val)

		// If whitespace or comment, advance line count by counting newlines.
		if r.tokid == SKIP {
			self.loc += strings.Count(val, "\n")
			continue
		}

		// If got parseable token, pass it and line number to parser.
		// fmt.Println(rule.token, val, self.loc)
		self.token = val
		lval.val = val
		lval.loc = self.loc // give grammar rules access to loc
		return r.tokid
	}
}

func (self *mmLex) Error(s string) {}

func yaccParse(src string) (*Ast, *mmLex) {
	lex := mmLex{src, 0, 1, ""}
	if mmParse(&lex) == 0 {
		return &ast, nil
	}
	return nil, &lex 
}
