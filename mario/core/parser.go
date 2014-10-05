//
// Copyright (c) 2014 10X Technologies, Inc. All rights reserved.
//
// MRO semantic checking.
//
package core

import (
	"fmt"
	"path/filepath"
	"strings"
)

//
// Semantic Checking Methods
//
func (global *Ast) err(locable Locatable, msg string, v ...interface{}) error {
	return &AstError{global, locable, fmt.Sprintf(msg, v...)}
}

func (callables *Callables) check(global *Ast) error {
	for _, callable := range callables.list {
		// Check for duplicates
		if _, ok := callables.table[callable.GetId()]; ok {
			return global.err(callable, "DuplicateNameError: stage or pipeline '%s' was already declared when encountered again", callable.GetId())
		}
		callables.table[callable.GetId()] = callable
	}
	return nil
}

func (params *Params) check(global *Ast) error {
	for _, param := range params.list {
		// Check for duplicates
		if _, ok := params.table[param.Id()]; ok {
			return global.err(param, "DuplicateNameError: parameter '%s' was already declared when encountered again", param.Id())
		}
		params.table[param.Id()] = param

		// Check that types exist.
		if _, ok := global.typeTable[param.Tname()]; !ok {
			return global.err(param, "TypeError: undefined type '%s'", param.Tname())
		}

		// Cache if param is file or path.
		_, ok := global.FiletypeTable[param.Tname()]
		param.SetIsFile(ok)

	}
	return nil
}

func (exp *ValExp) ResolveType(global *Ast, callable Callable) ([]string, error) {
	switch exp.GetKind() {

	// Handle scalar types.
	case "int", "float", "bool", "path", "null":
		return []string{exp.GetKind()}, nil

	// Handle strings (which could be files too).
	case "string":
		for filetype, _ := range global.FiletypeTable {
			if strings.HasSuffix(exp.Value.(string), filetype) {
				return []string{"string", filetype}, nil
			}
		}
		return []string{"string"}, nil

	// Array: [ 1, 2 ]
	case "array":
		for _, subexp := range exp.Value.([]Exp) {
			return []string{subexp.GetKind()}, nil
		}
		return []string{"null"}, nil
	// File: look for matching filetype in type table
	case "file":
		for filetype, _ := range global.FiletypeTable {
			if strings.HasSuffix(exp.Value.(string), filetype) {
				return []string{filetype}, nil
			}
		}
	}
	return []string{"unknown"}, nil
}

func (exp *RefExp) ResolveType(global *Ast, callable Callable) ([]string, error) {
	if callable == nil {
		global.err(exp, "ReferenceError: this binding cannot be resolved outside of a stage or pipeline.")
	}

	switch exp.GetKind() {

	// Param: self.myparam
	case "self":
		param, ok := callable.InParams().table[exp.Id]
		if !ok {
			return []string{""}, global.err(exp, "ScopeNameError: '%s' is not an input parameter of pipeline '%s'", exp.Id, callable.GetId())
		}
		return []string{param.Tname()}, nil

	// Call: STAGE.myoutparam or STAGE
	case "call":
		// Check referenced callable is acutally called in this scope.
		pipeline, ok := callable.(*Pipeline)
		if !ok {
			return []string{""}, global.err(exp, "ScopeNameError: '%s' is not called in pipeline '%s'", exp.Id, callable.GetId())
		} else {
			callable, ok := pipeline.callables.table[exp.Id]
			if !ok {
				return []string{""}, global.err(exp, "ScopeNameError: '%s' is not called in pipeline '%s'", exp.Id, pipeline.Id)
			}
			// Check referenced output is actually an output of the callable.
			param, ok := callable.OutParams().table[exp.outputId]
			if !ok {
				return []string{""}, global.err(exp, "NoSuchOutputError: '%s' is not an output parameter of '%s'", exp.outputId, callable.GetId())
			}

			return []string{param.Tname()}, nil
		}
	}
	return []string{"call"}, nil
}

func checkTypeMatch(paramType string, valueType string) bool {
	return (valueType == "null" ||
		paramType == valueType ||
		(paramType == "path" && valueType == "string"))
}

func (bindings *BindStms) check(global *Ast, callable Callable, params *Params) error {
	// Check the bindings
	for _, binding := range bindings.List {
		// Collect bindings by id so we can check that all params are bound.
		if _, ok := bindings.table[binding.id]; ok {
			return global.err(binding, "DuplicateBinding: '%s' already bound in this call", binding.id)
		}
		bindings.table[binding.id] = binding

		// Make sure the bound-to id is a declared parameter of the callable.
		param, ok := params.table[binding.id]
		if !ok {
			return global.err(binding, "ArgumentError: '%s' is not a valid parameter", binding.id)
		}

		// Typecheck the binding and cache the type.
		valueTypes, err := binding.Exp.ResolveType(global, callable)
		if err != nil {
			return err
		}
		anymatch := false
		lastType := ""
		for _, valueType := range valueTypes {
			anymatch = anymatch || checkTypeMatch(param.Tname(), valueType)
			lastType = valueType
		}
		if !anymatch {
			return global.err(param, "TypeMismatchError: expected type '%s' for '%s' but got '%s' instead", param.Tname(), param.Id(), lastType)
		}
		binding.Tname = param.Tname()
	}

	// Check that all input params of the called segment are bound.
	for _, param := range params.list {
		if _, ok := bindings.table[param.Id()]; !ok {
			return global.err(param, "ArgumentNotSuppliedError: no argument supplied for parameter '%s'", param.Id())
		}
	}
	return nil
}

func (global *Ast) check(incPaths []string, checkSrcPath bool) error {
	// Build type table, starting with builtins. Duplicates allowed.
	types := []string{"string", "int", "float", "bool", "path", "file", "map"}
	for _, filetype := range global.filetypes {
		types = append(types, filetype.Id)
		global.FiletypeTable[filetype.Id] = true
	}
	for _, t := range types {
		global.typeTable[t] = true
	}

	// Check for duplicate names amongst callables.
	if err := global.callables.check(global); err != nil {
		return err
	}

	// Check stage declarations.
	for _, stage := range global.Stages {
		// Check in parameters.
		if err := stage.inParams.check(global); err != nil {
			return err
		}
		// Check out parameters.
		if err := stage.outParams.check(global); err != nil {
			return err
		}
		if checkSrcPath {
			// Check existence of src path.
			if srcPath, found := searchPaths(stage.src.path, incPaths); !found {
				return global.err(stage, "SourcePathError: stage source path does not exist '%s'", srcPath)
			}
		}
		// Check split parameters.
		if stage.splitParams != nil {
			if err := stage.splitParams.check(global); err != nil {
				return err
			}
		}
	}

	// Check pipeline declarations.
	for _, pipeline := range global.Pipelines {
		// Check in parameters.
		if err := pipeline.inParams.check(global); err != nil {
			return err
		}
		// Check out parameters.
		if err := pipeline.outParams.check(global); err != nil {
			return err
		}

		// Check calls.
		for _, call := range pipeline.Calls {
			// Check for duplicate calls.
			if _, ok := pipeline.callables.table[call.Id]; ok {
				return global.err(call, "DuplicateCallError: '%s' was already called when encountered again", call.Id)
			}
			// Check we're calling something declared.
			callable, ok := global.callables.table[call.Id]
			if !ok {
				return global.err(call, "ScopeNameError: '%s' is not defined in this scope", call.Id)
			}
			// Save the valid callables for this scope.
			pipeline.callables.table[call.Id] = callable

			// Check the bindings
			if err := call.Bindings.check(global, pipeline, callable.InParams()); err != nil {
				return err
			}

			// Check that all input params of the callable are bound.
			for _, param := range callable.InParams().list {
				if _, ok := call.Bindings.table[param.Id()]; !ok {
					return global.err(call, "ArgumentNotSuppliedError: no argument supplied for parameter '%s'", param.Id())
				}
			}
		}
	}

	// Doing these in a separate loop gives the user better incremental
	// error messages while writing a long pipeline declaration.
	for _, pipeline := range global.Pipelines {
		// Check all pipeline input params are bound in a call statement.
		boundParamIds := map[string]bool{}
		for _, call := range pipeline.Calls {
			for _, binding := range call.Bindings.List {
				refexp, ok := binding.Exp.(*RefExp)
				if ok {
					boundParamIds[refexp.Id] = true
				}
			}
		}
		for _, param := range pipeline.inParams.list {
			if _, ok := boundParamIds[param.Id()]; !ok {
				return global.err(param, "UnusedInputError: no calls use pipeline input parameter '%s'", param.Id())
			}
		}

		// Check all pipeline output params are returned.
		returnedParamIds := map[string]bool{}
		for _, binding := range pipeline.ret.bindings.List {
			returnedParamIds[binding.id] = true
		}
		for _, param := range pipeline.outParams.list {
			if _, ok := returnedParamIds[param.Id()]; !ok {
				return global.err(pipeline.ret, "ReturnError: pipeline output parameter '%s' is not returned", param.Id())
			}
		}

		// Check return bindings.
		if err := pipeline.ret.bindings.check(global, pipeline, pipeline.outParams); err != nil {
			return err
		}
	}

	// If call statement present, check its bindings.
	if global.call != nil {
		callable := global.callables.table[global.call.Id]
		if err := global.call.Bindings.check(global, callable, callable.InParams()); err != nil {
			return err
		}
	}

	return nil
}

//
// Parser interface, called by runtime.
//
func parseSource(src string, srcPath string, incPaths []string, checkSrc bool) (string, *Ast, error) {
	// Preprocess: generate new source and a locmap.
	postsrc, locmap, err := preprocess(src, filepath.Base(srcPath),
		append([]string{filepath.Dir(srcPath)}, incPaths...))
	if err != nil {
		return "", nil, err
	}
	//printSourceMap(postsrc, locmap)

	// Parse the source into an AST and attach the locmap.
	ast, perr := yaccParse(postsrc)
	if perr != nil { // err is an mmLexInfo struct
		return "", nil, &ParseError{perr.token, locmap[perr.loc].fname, locmap[perr.loc].loc}
	}
	ast.locmap = locmap

	// Run semantic checks.
	if err := ast.check(incPaths, checkSrc); err != nil {
		return "", nil, err
	}
	return postsrc, ast, nil
}
