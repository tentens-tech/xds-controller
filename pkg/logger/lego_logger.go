// Copyright 2025 The Envoy XDS Controller Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package logger provides logging utilities and adapters for the xDS controller.
// It includes adapters for third-party logging interfaces like LEGO's ACME client.
package logger

import (
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// LegoLogger implements the github.com/go-acme/lego/v4/log.Logger interface
// and forwards logs to our preferred logging system
type LegoLogger struct {
	logger logr.Logger
}

// NewLegoLogger creates a new LegoLogger instance using the configured logger
func NewLegoLogger() *LegoLogger {
	return &LegoLogger{
		logger: log.Log,
	}
}

// cleanMessage removes lego's default prefixes and formats
func (l LegoLogger) cleanMessage(msg string) string {
	// Remove [INFO] prefix
	msg = strings.TrimPrefix(msg, "[INFO] ")
	// Remove [DEBUG] prefix
	msg = strings.TrimPrefix(msg, "[DEBUG] ")
	// Remove acme: prefix
	msg = strings.TrimPrefix(msg, "acme: ")
	return msg
}

// Fatal logs a message at level Fatal on the standard logger.
func (l LegoLogger) Fatal(args ...interface{}) {
	msg := fmt.Sprint(args...)
	l.logger.Error(nil, l.cleanMessage(msg))
}

// Fatalln logs a message at level Fatal on the standard logger.
func (l LegoLogger) Fatalln(args ...interface{}) {
	msg := fmt.Sprint(args...)
	l.logger.Error(nil, l.cleanMessage(msg))
}

// Fatalf logs a message at level Fatal on the standard logger.
func (l LegoLogger) Fatalf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	l.logger.Error(nil, l.cleanMessage(msg))
}

// Print logs a message at level Info on the standard logger.
func (l LegoLogger) Print(args ...interface{}) {
	msg := fmt.Sprint(args...)
	l.logger.V(2).Info(l.cleanMessage(msg))
}

// Println logs a message at level Info on the standard logger.
func (l LegoLogger) Println(args ...interface{}) {
	msg := fmt.Sprint(args...)
	l.logger.V(2).Info(l.cleanMessage(msg))
}

// Printf logs a message at level Info on the standard logger.
func (l LegoLogger) Printf(format string, args ...interface{}) {
	// First format the message with the args
	msg := fmt.Sprintf(format, args...)
	// Extract the domain name if present
	var domain string
	if len(args) > 0 {
		if s, ok := args[0].(string); ok {
			domain = s
		}
	}

	// Clean and format the message
	cleanMsg := l.cleanMessage(msg)

	if domain != "" {
		l.logger.V(2).Info(cleanMsg, "domain", domain)
	} else {
		l.logger.V(2).Info(cleanMsg)
	}
}
