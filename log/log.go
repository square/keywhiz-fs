// Copyright 2015 Square Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package log

import (
	"fmt"
	"log"
	"log/syslog"
	"os"
	"time"
)

const (
	// Default syslog facility which is logged to.
	defaultSyslogFacility = syslog.LOG_USER

	// Maximum backlog of queued messages
	workQueueMaxBacklog = 25
)

// Logger maintains state of log emitters for different severity levels.
type Logger struct {
	syslog   *syslog.Writer
	errorLog *log.Logger
	warnLog  *log.Logger
	infoLog  *log.Logger
	debugLog *log.Logger
	queue    chan func()
	debug    bool
}

// Config contains values necessary for configurating a logger.
type Config struct {
	Debug      bool
	Mountpoint string
	Syslog     bool
}

// New initializes a Logger for a given component and with debugging output on/off.
func New(component string, config Config) *Logger {
	name := fmt.Sprintf("%s[%s]", component, config.Mountpoint)

	flags := log.LstdFlags
	errorLog := log.New(os.Stderr, fmt.Sprintf("ERROR %v: ", name), flags)
	warnLog := log.New(os.Stderr, fmt.Sprintf("WARN %v: ", name), flags)
	infoLog := log.New(os.Stdout, fmt.Sprintf("INFO %v: ", name), flags)
	debugLog := log.New(os.Stdout, fmt.Sprintf("DEBUG %v: ", name), flags)

	var syslogWriter *syslog.Writer
	if config.Syslog {
		var err error
		syslogWriter, err = syslog.New(syslog.LOG_NOTICE|defaultSyslogFacility, name)
		if err != nil {
			errorLog.Printf("Error starting syslog logging, continuing: %v\n", err)
			syslogWriter = nil
		}
	}

	queue := make(chan func(), workQueueMaxBacklog)
	logger := &Logger{syslogWriter, errorLog, warnLog, infoLog, debugLog, queue, config.Debug}
	go logger.process()
	return logger
}

// Enqueue work into logger queue. Best-effort; drops message if queue is full.
func (l Logger) nonBlockingEnqueue(worker func()) {
	select {
	case l.queue <- worker:
		// queued
	default:
		// queue is full; possibly because syslog is stuck.
		fmt.Fprintf(
			os.Stderr,
			"** dropping log message at %s, buffer full (%d queued) **",
			time.Now().Format(time.UnixDate), len(l.queue))
	}
}

// Process work queue. Should run in async goroutine.
func (l Logger) process() {
	for {
		worker, more := <-l.queue
		if !more {
			return
		}
		worker()
	}
}

// Errorf emits messages at ERROR level with a printf style interface.
func (l Logger) Errorf(format string, v ...interface{}) {
	worker := func() {
		msg := fmt.Sprintf(format, v...)
		if l.syslog != nil {
			l.syslog.Err(msg)
		} else {
			l.errorLog.Println(msg)
		}
	}
	l.nonBlockingEnqueue(worker)
}

// Warnf emits messages at WARN level with a printf style interface.
func (l Logger) Warnf(format string, v ...interface{}) {
	worker := func() {
		msg := fmt.Sprintf(format, v...)
		if l.syslog != nil {
			l.syslog.Warning(msg)
		} else {
			l.warnLog.Println(msg)
		}
	}
	l.nonBlockingEnqueue(worker)
}

// Infof emits messages at INFO level with a printf style interface.
func (l Logger) Infof(format string, v ...interface{}) {
	worker := func() {
		msg := fmt.Sprintf(format, v...)
		if l.syslog != nil {
			l.syslog.Info(msg)
		} else {
			l.infoLog.Println(msg)
		}
	}
	l.nonBlockingEnqueue(worker)
}

// Debugf emits messages at DEBUG level with a printf style interface if debugging was enabled.
func (l Logger) Debugf(format string, v ...interface{}) {
	worker := func() {
		if l.debug {
			msg := fmt.Sprintf(format, v...)
			if l.syslog != nil {
				l.syslog.Debug(msg)
			} else {
				l.debugLog.Println(msg)
			}
		}
	}
	l.nonBlockingEnqueue(worker)
}

// Close closes any internal writers.
func (l Logger) Close() error {
	close(l.queue)
	if l.syslog != nil {
		return l.syslog.Close()
	}
	return nil
}
