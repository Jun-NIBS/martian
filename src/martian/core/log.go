//
// Copyright (c) 2014 10X Genomics, Inc. All rights reserved.
//
// Martian logging.
//
package core

import (
	"fmt"
	"io"
	"os"
)

type Logger struct {
	stdoutWriter io.Writer
	fileWriter   io.Writer
	cache        string
}

var ENABLE_LOGGING bool = true
var LOGGER *Logger = nil

func logInit() bool {
	if ENABLE_LOGGING {
		if LOGGER == nil {
			LOGGER = &Logger{io.Writer(os.Stdout), nil, ""}
		}
		return true
	}
	return false
}

func log(msg string) {
	if logInit() {
		if LOGGER.fileWriter != nil {
			LOGGER.fileWriter.Write([]byte(msg))
		} else {
			LOGGER.cache += msg
		}
	}
}

func print(msg string) {
	if logInit() {
		LOGGER.stdoutWriter.Write([]byte(msg))
		log(msg)
	}
}

func LogTee(filename string) {
	if logInit() {
		if LOGGER.fileWriter == nil {
			logInit()
			f, _ := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE, 0644)
			LOGGER.fileWriter = io.Writer(f)
			log(LOGGER.cache)
		}
	}
}

func formatRaw(format string, v ...interface{}) string {
	return fmt.Sprintf(format, v...)
}

func formatInfo(component string, format string, v ...interface{}) string {
	return fmt.Sprintf("%s [%s] %s\n", Timestamp(), component, fmt.Sprintf(format, v...))
}

func formatError(err error, component string, format string, v ...interface{}) string {
	return fmt.Sprintf("%s [%s] %s\n          %s\n", Timestamp(), component, fmt.Sprintf(format, v...), err.Error())
}

func Log(format string, v ...interface{}) {
	log(formatRaw(format, v...))
}

func LogInfo(component string, format string, v ...interface{}) {
	log(formatInfo(component, format, v...))
}

func LogError(err error, component string, format string, v ...interface{}) {
	log(formatError(err, component, format, v...))
}

func Print(format string, v ...interface{}) {
	print(formatRaw(format, v...))
}

func Println(format string, v ...interface{}) {
	print(formatRaw(format, v...) + "\n")
}

func PrintInfo(component string, format string, v ...interface{}) {
	print(formatInfo(component, format, v...))
}

func PrintError(err error, component string, format string, v ...interface{}) {
	print(formatError(err, component, format, v...))
}
