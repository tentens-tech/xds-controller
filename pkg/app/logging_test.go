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
	"math"
	"testing"

	"go.uber.org/zap/zapcore"
)

func TestZapLevel(t *testing.T) {
	tests := []struct {
		in   int
		want zapcore.Level
	}{
		{-1, zapcore.DebugLevel},
		{0, zapcore.InfoLevel},
		{5, zapcore.FatalLevel},
		{-3, zapcore.Level(-3)},
		{300, zapcore.Level(math.MaxInt8)},
		{-300, zapcore.Level(math.MinInt8)},
	}
	for _, tt := range tests {
		if got := zapLevel(tt.in); got != tt.want {
			t.Errorf("zapLevel(%d) = %d, want %d", tt.in, got, tt.want)
		}
	}
}
