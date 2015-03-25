//
// Copyright (c) 2014 10X Genomics, Inc. All rights reserved.
//
// MRO semantic checking.
//
package core

import (
	"fmt"
	"os"
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
		if _, ok := callables.table[callable.getId()]; ok {
			return global.err(callable, "DuplicateNameError: stage or pipeline '%s' was already declared when encountered again", callable.getId())
		}
		callables.table[callable.getId()] = callable
	}
	return nil
}

func (params *Params) check(global *Ast) error {
	for _, param := range params.list {
		// Check for duplicates
		if _, ok := params.table[param.getId()]; ok {
			return global.err(param, "DuplicateNameError: parameter '%s' was already declared when encountered again", param.getId())
		}
		params.table[param.getId()] = param

		// Check that types exist.
		if _, ok := global.typeTable[param.getTname()]; !ok {
			return global.err(param, "TypeError: undefined type '%s'", param.getTname())
		}

		// Cache if param is file or path.
		_, ok := global.filetypeTable[param.getTname()]
		param.setIsFile(ok)

	}
	return nil
}

func (exp *ValExp) resolveType(global *Ast, callable Callable) ([]string, int, error) {
	switch exp.getKind() {

	// Handle scalar types.
	case "int", "float", "bool", "path", "map", "null":
		return []string{exp.getKind()}, 0, nil

	// Handle strings (which could be files too).
	case "string":
		for filetype, _ := range global.filetypeTable {
			if strings.HasSuffix(exp.value.(string), filetype) {
				return []string{"string", filetype}, 0, nil
			}
		}
		return []string{"string"}, 0, nil

	// Array: [ 1, 2 ]
	case "array":
		for _, subexp := range exp.value.([]Exp) {
			arrayKind, arrayDim, err := subexp.resolveType(global, callable)
			return arrayKind, arrayDim + 1, err
		}
		return []string{"null"}, 1, nil
	// File: look for matching filetype in type table
	case "file":
		for filetype, _ := range global.filetypeTable {
			if strings.HasSuffix(exp.value.(string), filetype) {
				return []string{filetype}, 0, nil
			}
		}
	}
	return []string{"unknown"}, 0, nil
}

func (exp *RefExp) resolveType(global *Ast, callable Callable) ([]string, int, error) {
	if callable == nil {
		global.err(exp, "ReferenceError: this binding cannot be resolved outside of a stage or pipeline.")
	}

	switch exp.getKind() {

	// Param: self.myparam
	case "self":
		param, ok := callable.getInParams().table[exp.id]
		if !ok {
			return []string{""}, 0, global.err(exp, "ScopeNameError: '%s' is not an input parameter of pipeline '%s'", exp.id, callable.getId())
		}
		return []string{param.getTname()}, param.getArrayDim(), nil

	// Call: STAGE.myoutparam or STAGE
	case "call":
		// Check referenced callable is acutally called in this scope.
		pipeline, ok := callable.(*Pipeline)
		if !ok {
			return []string{""}, 0, global.err(exp, "ScopeNameError: '%s' is not called in pipeline '%s'", exp.id, callable.getId())
		} else {
			callable, ok := pipeline.callables.table[exp.id]
			if !ok {
				return []string{""}, 0, global.err(exp, "ScopeNameError: '%s' is not called in pipeline '%s'", exp.id, pipeline.id)
			}
			// Check referenced output is actually an output of the callable.
			param, ok := callable.getOutParams().table[exp.outputId]
			if !ok {
				return []string{""}, 0, global.err(exp, "NoSuchOutputError: '%s' is not an output parameter of '%s'", exp.outputId, callable.getId())
			}

			return []string{param.getTname()}, param.getArrayDim(), nil
		}
	}
	return []string{"unknown"}, 0, nil
}

func checkTypeMatch(paramType string, valueType string) bool {
	return (valueType == "null" ||
		paramType == valueType ||
		(paramType == "path" && valueType == "string"))
}

func (bindings *BindStms) check(global *Ast, callable Callable, params *Params) error {
	// Check the bindings
	for _, binding := range bindings.list {
		// Collect bindings by id so we can check that all params are bound.
		if _, ok := bindings.table[binding.id]; ok {
			return global.err(binding, "DuplicateBinding: '%s' already bound in this call", binding.id)
		}
		// Building the bindings table could also happen in the grammar rules,
		// but then we lose the ability to detect duplicate parameters as we're
		// doing right above this comment. So leave this here.
		bindings.table[binding.id] = binding

		// Make sure the bound-to id is a declared parameter of the callable.
		param, ok := params.table[binding.id]
		if !ok {
			return global.err(binding, "ArgumentError: '%s' is not a valid parameter", binding.id)
		}

		// Typecheck the binding and cache the type.
		valueTypes, arrayDim, err := binding.exp.resolveType(global, callable)
		if err != nil {
			return err
		}

		// Check for array match
		if binding.sweep {
			if arrayDim == 0 {
				return global.err(binding, "TypeMismatchError: got non-array value for sweep parameter '%s'", param.getId())
			}
			arrayDim -= 1
		}
		if param.getArrayDim() != arrayDim {
			if param.getArrayDim() == 0 && arrayDim > 0 {
				return global.err(binding, "TypeMismatchError: got array value for non-array parameter '%s'", param.getId())
			} else if param.getArrayDim() > 0 && arrayDim == 0 {
				// Allow an array-decorated parameter to accept null values.
				if len(valueTypes) < 1 || valueTypes[0] != "null" {
					return global.err(binding, "TypeMismatchError: expected array of '%s' for '%s'", param.getTname(), param.getId())
				}
			} else {
				return global.err(binding, "TypeMismatchError: got %d-dimensional array value for %d-dimensional array parameter '%s'", arrayDim, param.getArrayDim(), param.getId())
			}
		}

		anymatch := false
		lastType := ""
		for _, valueType := range valueTypes {
			anymatch = anymatch || checkTypeMatch(param.getTname(), valueType)
			lastType = valueType
		}
		if !anymatch {
			return global.err(param, "TypeMismatchError: expected type '%s' for '%s' but got '%s' instead", param.getTname(), param.getId(), lastType)
		}
		binding.tname = param.getTname()
	}

	// Check that all input params of the called segment are bound.
	for _, param := range params.list {
		if _, ok := bindings.table[param.getId()]; !ok {
			return global.err(param, "ArgumentNotSuppliedError: no argument supplied for parameter '%s'", param.getId())
		}
	}
	return nil
}

func (global *Ast) check(stagecodePaths []string, checkSrcPath bool) error {
	// Build type table, starting with builtins. Duplicates allowed.
	types := []string{"string", "int", "float", "bool", "path", "file", "map"}
	for _, filetype := range global.filetypes {
		types = append(types, filetype.id)
		global.filetypeTable[filetype.id] = true
	}
	for _, t := range types {
		global.typeTable[t] = true
	}

	// Check for duplicate names amongst callables.
	if err := global.callables.check(global); err != nil {
		return err
	}

	// Check stage declarations.
	for _, stage := range global.stages {
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
			if _, found := searchPaths(stage.src.path, stagecodePaths); !found {
				stagecodePathsList := strings.Join(stagecodePaths, ", ")
				return global.err(stage, "SourcePathError: searched (%s) but stage source path not found '%s'", stagecodePathsList, stage.src.path)
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
	for _, pipeline := range global.pipelines {
		// Check in parameters.
		if err := pipeline.inParams.check(global); err != nil {
			return err
		}
		// Check out parameters.
		if err := pipeline.outParams.check(global); err != nil {
			return err
		}

		preflightCalls := []*CallStm{}

		// Check calls.
		for _, call := range pipeline.calls {
			// Check for duplicate calls.
			if _, ok := pipeline.callables.table[call.id]; ok {
				return global.err(call, "DuplicateCallError: '%s' was already called when encountered again", call.id)
			}
			// Check we're calling something declared.
			callable, ok := global.callables.table[call.id]
			if !ok {
				return global.err(call, "ScopeNameError: '%s' is not defined in this scope", call.id)
			}
			// Save the valid callables for this scope.
			pipeline.callables.table[call.id] = callable

			// Check to make sure if local, preflight or volatile is declared, callable is a stage
			if _, ok := callable.(*Stage); !ok {
				if call.modifiers.local {
					return global.err(call, "UnsupportedTagError: Pipeline '%s' cannot be called with 'local' tag", call.id)
				}
				if call.modifiers.preflight {
					return global.err(call, "UnsupportedTagError: Pipeline '%s' cannot be called with 'preflight' tag", call.id)
				}
				if call.modifiers.volatile {
					return global.err(call, "UnsupportedTagError: Pipeline '%s' cannot be called with 'volatile' tag", call.id)
				}
			}
			if call.modifiers.preflight {
				for _, binding := range call.bindings.list {
					if binding.exp.getKind() == "call" {
						return global.err(call, "PreflightBindingError: Preflight stage '%s' cannot have input parameter bound to output parameter of another stage or pipeline", call.id)
					}
				}
				if len(callable.getOutParams().list) > 0 {
					return global.err(call, "PreflightOutputError: Preflight stage '%s' cannot have any output parameters", call.id)
				}

				preflightCalls = append(preflightCalls, call)
			}

			// Check the bindings
			if err := call.bindings.check(global, pipeline, callable.getInParams()); err != nil {
				return err
			}

			// Check that all input params of the callable are bound.
			for _, param := range callable.getInParams().list {
				if _, ok := call.bindings.table[param.getId()]; !ok {
					return global.err(call, "ArgumentNotSuppliedError: no argument supplied for parameter '%s'", param.getId())
				}
			}
		}
	}

	// Doing these in a separate loop gives the user better incremental
	// error messages while writing a long pipeline declaration.
	for _, pipeline := range global.pipelines {
		// Check all pipeline input params are bound in a call statement.
		boundParamIds := map[string]bool{}
		for _, call := range pipeline.calls {
			for _, binding := range call.bindings.list {
				refexp, ok := binding.exp.(*RefExp)
				if ok {
					boundParamIds[refexp.id] = true
				}
			}
		}
		for _, param := range pipeline.inParams.list {
			if _, ok := boundParamIds[param.getId()]; !ok {
				return global.err(param, "UnusedInputError: no calls use pipeline input parameter '%s'", param.getId())
			}
		}

		// Check all pipeline output params are returned.
		returnedParamIds := map[string]bool{}
		for _, binding := range pipeline.ret.bindings.list {
			returnedParamIds[binding.id] = true
		}
		for _, param := range pipeline.outParams.list {
			if _, ok := returnedParamIds[param.getId()]; !ok {
				return global.err(pipeline.ret, "ReturnError: pipeline output parameter '%s' is not returned", param.getId())
			}
		}

		// Check return bindings.
		if err := pipeline.ret.bindings.check(global, pipeline, pipeline.outParams); err != nil {
			return err
		}
	}

	// If call statement present, check the call and its bindings.
	if global.call != nil {
		callable, ok := global.callables.table[global.call.id]
		if !ok {
			return global.err(global.call, "ScopeNameError: '%s' is not defined in this scope", global.call.id)
		}
		if err := global.call.bindings.check(global, callable, callable.getInParams()); err != nil {
			return err
		}
	}

	return nil
}

//
// Parser interface, called by runtime.
//
func parseSource(src string, srcPath string, incPaths []string, checkSrc bool) (string, []string, *Ast, error) {
	// Add the source file's own folder to the include path for
	// resolving both @includes and stage src paths.
	incPaths = append([]string{filepath.Dir(srcPath)}, incPaths...)

	// Add PATH environment variable to the stage code path
	stagecodePaths := append(incPaths, strings.Split(os.Getenv("PATH"), ":")...)

	// Preprocess: generate new source and a locmap.
	postsrc, ifnames, locmap, err := preprocess(src, filepath.Base(srcPath), incPaths)
	if err != nil {
		return "", nil, nil, err
	}
	//printSourceMap(postsrc, locmap)

	// Parse the source into an AST and attach the locmap.
	ast, perr := yaccParse(postsrc)
	if perr != nil { // err is an mmLexInfo struct
		// Guard against index out of range, which can happen if there is syntax error
		// at the end of the file, e.g. forgetting to put a close paren at the end of
		// and invocation call/file.
		if perr.loc >= len(locmap) {
			perr.loc = len(locmap) - 1
		}
		return "", nil, nil, &ParseError{perr.token, locmap[perr.loc].fname, locmap[perr.loc].loc}
	}
	ast.locmap = locmap

	// Run semantic checks.
	if err := ast.check(stagecodePaths, checkSrc); err != nil {
		return "", nil, nil, err
	}
	return postsrc, ifnames, ast, nil
}
