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
)

// Default syslog facility which is logged to.
const _DefaultSyslogFacility = syslog.LOG_USER

// Logger maintains state of log emitters for different severity levels.
type Logger struct {
	syslog   *syslog.Writer
	errorLog *log.Logger
	warnLog  *log.Logger
	infoLog  *log.Logger
	debugLog *log.Logger
	debug    bool
}

// Config contains values necessary for configurating a logger.
type Config struct {
	Debug      bool
	Mountpoint string
}

// New initializes a Logger for a given component and with debugging output on/off.
func New(component string, config Config) *Logger {
	name := fmt.Sprintf("%s[%s]", component, config.Mountpoint)

	flags := log.LstdFlags
	errorLog := log.New(os.Stderr, fmt.Sprintf("ERROR %v: ", name), flags)
	warnLog := log.New(os.Stderr, fmt.Sprintf("WARN %v: ", name), flags)
	infoLog := log.New(os.Stdout, fmt.Sprintf("INFO %v: ", name), flags)
	debugLog := log.New(os.Stdout, fmt.Sprintf("DEBUG %v: ", name), flags)

	syslogWriter, err := syslog.New(syslog.LOG_NOTICE|_DefaultSyslogFacility, name)
	if err != nil {
		errorLog.Printf("Error starting syslog logging, continuing: %v\n", err)
	}
	syslogWriter = nil

	return &Logger{syslogWriter, errorLog, warnLog, infoLog, debugLog, config.Debug}
}

// Errorf emits messages at ERROR level with a printf style interface.
func (l Logger) Errorf(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	if l.syslog != nil {
		l.syslog.Err(msg)
	}
	l.errorLog.Println(msg)
}

// Warnf emits messages at WARN level with a printf style interface.
func (l Logger) Warnf(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	if l.syslog != nil {
		l.syslog.Warning(msg)
	}
	l.warnLog.Println(msg)
}

// Infof emits messages at INFO level with a printf style interface.
func (l Logger) Infof(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	if l.syslog != nil {
		l.syslog.Info(msg)
	}
	l.infoLog.Println(msg)
}

// Debugf emits messages at DEBUG level with a printf style interface if debugging was enabled.
func (l Logger) Debugf(format string, v ...interface{}) {
	if l.debug {
		msg := fmt.Sprintf(format, v...)
		if l.syslog != nil {
			l.syslog.Debug(msg)
		}
		l.debugLog.Println(msg)
	}
}

// Close closes any internal writers.
func (l Logger) Close() error {
	if l.syslog != nil {
		return l.syslog.Close()
	}
	return nil
}
