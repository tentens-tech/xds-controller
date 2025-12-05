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

// Package logfilter provides log filtering utilities for the xDS controller.
// It includes filters to reduce noise from health check logs.
package logfilter

import "go.uber.org/zap/zapcore"

// HealthzFilterCore wraps a zapcore.Core to filter out controller-runtime.healthz logs.
type HealthzFilterCore struct {
	zapcore.Core
}

func (c *HealthzFilterCore) Check(entry zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	// Filter out controller-runtime.healthz logs at INFO and WARN level
	if entry.LoggerName == "controller-runtime.healthz" && (entry.Level == zapcore.InfoLevel || entry.Level == zapcore.WarnLevel) {
		return ce
	}
	return c.Core.Check(entry, ce)
}

func (c *HealthzFilterCore) With(fields []zapcore.Field) zapcore.Core {
	return &HealthzFilterCore{c.Core.With(fields)}
}
