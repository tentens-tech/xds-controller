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

package app

import (
	"flag"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	crtzap "sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/tentens-tech/xds-controller/pkg/logfilter"
)

// SetupLogger configures and returns a logger based on the provided options.
func SetupLogger(opts *Options, fs *flag.FlagSet) logr.Logger {
	zapOpts := crtzap.Options{
		Development: !opts.Production,
		TimeEncoder: zapcore.ISO8601TimeEncoder,
		Level:       zapcore.Level(opts.LogLevel),
		EncoderConfigOptions: []crtzap.EncoderConfigOption{
			func(config *zapcore.EncoderConfig) {
				config.LevelKey = "level"
				config.EncodeLevel = customLevelEncoder()
			},
		},
	}

	// Bind zap-specific flags if flag set provided
	if fs != nil {
		zapOpts.BindFlags(fs)
	}

	baseLogger := crtzap.NewRaw(crtzap.UseFlagOptions(&zapOpts))

	// Filter out noisy controller-runtime.healthz logs
	filteredCore := &logfilter.HealthzFilterCore{Core: baseLogger.Core()}
	zapLogger := zap.New(filteredCore)

	return zapr.NewLogger(zapLogger)
}

// customLevelEncoder returns a custom zapcore level encoder that maps
// negative log levels to human-readable names.
func customLevelEncoder() zapcore.LevelEncoder {
	return func(level zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
		switch level {
		case zapcore.Level(-1):
			enc.AppendString("WARN")
		case zapcore.Level(-2):
			enc.AppendString("DEBUG")
		case zapcore.Level(-3):
			enc.AppendString("VERBOSE")
		default:
			zapcore.CapitalLevelEncoder(level, enc)
		}
	}
}
