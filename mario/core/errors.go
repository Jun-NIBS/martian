//
// Copyright (c) 2014 10X Technologies, Inc. All rights reserved.
//
// Mario error types.
//
package core

import (
	"fmt"
	"os"
	"strings"
)

//
// Mario Errors
//

// MarioError
type MarioError struct {
	Msg string
}

func (self *MarioError) Error() string {
	return self.Msg
}

// RuntimeError
type RuntimeError struct {
	Msg string
}

func (self *RuntimeError) Error() string {
	return fmt.Sprintf("RuntimeError: %s.", self.Msg)
}

// PipestanceExistsError
type PipestanceExistsError struct {
	psid string
}

func (self *PipestanceExistsError) Error() string {
	return fmt.Sprintf("RuntimeError: pipestance '%s' already exists.", self.psid)
}

// PreprocessError
type PreprocessError struct {
	files []string
}

func (self *PreprocessError) Error() string {
	return fmt.Sprintf("@include file not found: %s", strings.Join(self.files, ", "))
}

// AstError
type AstError struct {
	global  *Ast
	locable Locatable
	msg     string
}

func (self *AstError) Error() string {
	// If there's no newline at the end of the source and the error is in the
	// node at the end of the file, the loc can be one larger than the size
	// of the locmap. So cap it so we don't have an array out of bounds.
	loc := self.locable.getLoc()
	if loc >= len(self.global.locmap) {
		loc = len(self.global.locmap) - 1
	}
	return fmt.Sprintf("MRO %s at %s:%d.", self.msg,
		self.global.locmap[loc].fname,
		self.global.locmap[loc].loc)
}

// ParseError
type ParseError struct {
	token string
	fname string
	loc   int
}

func (self *ParseError) Error() string {
	return fmt.Sprintf("MRO ParseError: unexpected token '%s' at %s:%d.", self.token, self.fname, self.loc)
}

func DieIf(err error) {
	if err != nil {
		fmt.Println()
		fmt.Println(err.Error())
		os.Exit(1)
	}
}
