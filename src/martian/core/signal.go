//
// Copyright (c) 2014 10X Genomics, Inc. All rights reserved.
//
// Martian signal handler.
//
package core

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

type SignalHandler struct {
	count int
	exit  bool
	mutex *sync.Mutex
	block chan int
}

var signalHandler *SignalHandler = nil

func EnterCriticalSection() {
	signalHandler.mutex.Lock()
	if signalHandler.exit {
		// Block other goroutines from entering critical section if exit flag has been set
		signalHandler.mutex.Unlock()
		<-signalHandler.block
	}
	signalHandler.count += 1
	signalHandler.mutex.Unlock()
}

func ExitCriticalSection() {
	signalHandler.mutex.Lock()
	signalHandler.count -= 1
	signalHandler.mutex.Unlock()
}

func newSignalHandler() *SignalHandler {
	self := &SignalHandler{}
	self.mutex = &sync.Mutex{}
	self.block = make(chan int)
	return self
}

func SetupSignalHandlers() {
	// Handle CTRL-C and kill.
	sigchan := make(chan os.Signal, 1)
	signal.Notify(sigchan, os.Interrupt)
	signal.Notify(sigchan, syscall.SIGTERM)

	signalHandler = newSignalHandler()
	go func() {
		<-sigchan

		// Set exit flag
		signalHandler.mutex.Lock()
		signalHandler.exit = true

		// Make sure all goroutines have left critical sections
		for signalHandler.count > 0 {
			signalHandler.mutex.Unlock()
			time.Sleep(1)
			signalHandler.mutex.Lock()
		}
		os.Exit(1)
	}()
}
