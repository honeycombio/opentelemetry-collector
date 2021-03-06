// Copyright 2020 OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package testdata

import (
	"testing"

	"github.com/open-telemetry/opentelemetry-collector/internal/data"
	"github.com/stretchr/testify/assert"
)

func TestGenerateTraceData(t *testing.T) {
	assert.EqualValues(t, GenerateTraceDataOneEmptyResourceSpans(), GenerateTraceDataOneEmptyResourceSpans())
	assert.EqualValues(t, GenerateTraceDataNoLibraries(), GenerateTraceDataNoLibraries())
	assert.EqualValues(t, GenerateTraceDataNoSpans(), GenerateTraceDataNoSpans())
	assert.EqualValues(t, GenerateTraceDataOneSpanNoResource(), GenerateTraceDataOneSpanNoResource())
	assert.EqualValues(t, GenerateTraceDataOneSpan(), GenerateTraceDataOneSpan())
	assert.EqualValues(t, GenerateTraceDataSameResourceTwoSpans(), GenerateTraceDataSameResourceTwoSpans())
	assert.EqualValues(t, GenerateTraceDataTwoSpansSameResourceOneDifferent(), GenerateTraceDataTwoSpansSameResourceOneDifferent())
}

func TestGeneratedTraceOtlp(t *testing.T) {
	assert.EqualValues(t, generateTraceOtlpNoLibraries(), generateTraceOtlpNoLibraries())
	assert.EqualValues(t, generateTraceOtlpNoSpans(), generateTraceOtlpNoSpans())
	assert.EqualValues(t, generateTraceOtlpOneSpanNoResource(), generateTraceOtlpOneSpanNoResource())
	assert.EqualValues(t, generateTraceOtlpOneSpan(), generateTraceOtlpOneSpan())
	assert.EqualValues(t, generateTraceOtlpSameResourceTwoSpans(), generateTraceOtlpSameResourceTwoSpans())
	assert.EqualValues(t, generateTraceOtlpTwoSpansSameResourceOneDifferent(), generateTraceOtlpTwoSpansSameResourceOneDifferent())
}

func TestToFromOtlp(t *testing.T) {
	for i := range AllTraceTestCases {
		test := AllTraceTestCases[i]
		t.Run(test.Name, func(t *testing.T) {
			td := data.TraceDataFromOtlp(test.OtlpData)
			assert.EqualValues(t, test.TraceData, td)
			otlp := data.TraceDataToOtlp(td)
			assert.EqualValues(t, test.OtlpData, otlp)
		})
	}
}
