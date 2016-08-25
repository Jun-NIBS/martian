//
// Copyright (c) 2014 10X Genomics, Inc. All rights reserved.
//
// Martian error types.
//
package core

import (
	"fmt"
	"os"
	"strings"
)

//
// Martian Errors
//

// MartianError
type MartianError struct {
	Msg string
}

func (self *MartianError) Error() string {
	return self.Msg
}

// RuntimeError
type RuntimeError struct {
	Msg string
}

func (self *RuntimeError) Error() string {
	return fmt.Sprintf("RuntimeError: %s.", self.Msg)
}

// PipestanceInvocationError
type PipestanceInvocationError struct {
	Psid           string
	InvocationPath string
}

func (self *PipestanceInvocationError) Error() string {
	return fmt.Sprintf("RuntimeError: pipestance '%s' already exists with different invocation file %s",
		self.Psid, self.InvocationPath)
}

// PipestancePathError
type PipestancePathError struct {
	Path string
}

func (self *PipestancePathError) Error() string {
	return fmt.Sprintf("RuntimeError: %s is not a pipestance directory", self.Path)
}

// PipestanceJobModeError
type PipestanceJobModeError struct {
	Psid    string
	JobMode string
}

func (self *PipestanceJobModeError) Error() string {
	return fmt.Sprintf("RuntimeError: pipestance '%s' was originally started in job mode '%s'. Please try running again in job mode '%s'.", self.Psid, self.JobMode, self.JobMode)
}

// PipestanceLockedError
type PipestanceLockedError struct {
	Psid           string
	PipestancePath string
}

func (self *PipestanceLockedError) Error() string {
	return fmt.Sprintf("RuntimeError: pipestance '%s' already exists and is locked by another Martian instance. If you are sure no other Martian instance is running, delete the _lock file in %s and start Martian again.", self.Psid, self.PipestancePath)
}

// PipestanceNotFailedError
type PipestanceNotFailedError struct {
	Psid string
}

func (self *PipestanceNotFailedError) Error() string {
	return fmt.Sprintf("RuntimeError: pipestance '%s' is not failed.", self.Psid)
}

// PipestanceNotRunningError
type PipestanceNotRunningError struct {
	Psid string
}

func (self *PipestanceNotRunningError) Error() string {
	return fmt.Sprintf("RuntimeError: pipestance '%s' is not running.", self.Psid)
}

// PipestanceNotExistsError
type PipestanceNotExistsError struct {
	Psid string
}

func (self *PipestanceNotExistsError) Error() string {
	return fmt.Sprintf("RuntimeError: pipestance '%s' doesn't exist.", self.Psid)
}

// PipestanceExistsError
type PipestanceExistsError struct {
	Psid string
}

func (self *PipestanceExistsError) Error() string {
	return fmt.Sprintf("RuntimeError: pipestance '%s' already exists.", self.Psid)
}

// PipestanceSizeError
type PipestanceSizeError struct {
	Psid string
}

func (self *PipestanceSizeError) Error() string {
	return fmt.Sprintf("RuntimeError: Cannot compute size requirement for %s.", self.Psid)
}

// PipestanceCopyingError
type PipestanceCopyingError struct {
	Psid string
}

func (self *PipestanceCopyingError) Error() string {
	return fmt.Sprintf("RuntimeError: pipestance '%s' is currently being copied.", self.Psid)
}

// PipestanceWipeError
type PipestanceWipeError struct {
	Psid string
}

func (self *PipestanceWipeError) Error() string {
	return fmt.Sprintf("RuntimeError: pipestance '%s' cannot be wiped.", self.Psid)
}

// ZipError
type ZipError struct {
	ZipPath  string
	FilePath string
}

func (self *ZipError) Error() string {
	return fmt.Sprintf("ZipError: %s does not exist in %s", self.FilePath, self.ZipPath)
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
	global *Ast
	Node   *AstNode
	Msg    string
}

func (self *AstError) Error() string {
	return fmt.Sprintf("MRO %s at %s:%d.", self.Msg, self.Node.Fname, self.Node.Loc)
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
		fmt.Println()
		os.Exit(1)
	}
}
