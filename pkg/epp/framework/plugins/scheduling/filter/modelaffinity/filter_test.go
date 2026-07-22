/*
Copyright 2026 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package modelaffinity

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	k8stypes "k8s.io/apimachinery/pkg/types"

	fwkdl "github.com/llm-d/llm-d-router/pkg/epp/framework/interface/datalayer"
	fwkplugin "github.com/llm-d/llm-d-router/pkg/epp/framework/interface/plugin"
	"github.com/llm-d/llm-d-router/pkg/epp/framework/interface/scheduling"
	"github.com/llm-d/llm-d-router/test/utils"
)

func createEndpoint(name, address string, labels map[string]string) scheduling.Endpoint {
	return scheduling.NewEndpoint(
		&fwkdl.EndpointMetadata{
			NamespacedName: k8stypes.NamespacedName{Namespace: "default", Name: name},
			Address:        address,
			Labels:         labels,
		},
		&fwkdl.Metrics{},
		nil,
	)
}

func TestFactory(t *testing.T) {
	tests := []struct {
		name       string
		pluginName string
		jsonParams string
		wantErr    bool
	}{
		{
			name:       "valid with defaults",
			pluginName: "test-filter",
			jsonParams: `{}`,
			wantErr:    false,
		},
		{
			name:       "valid with custom label key",
			pluginName: "custom-key",
			jsonParams: `{"labelKey": "serving-model"}`,
			wantErr:    false,
		},
		{
			name:       "valid with custom label key dot-notation",
			pluginName: "full-config",
			jsonParams: `{"labelKey": "ai.model"}`,
			wantErr:    false,
		},
		{
			name:       "empty name should error",
			pluginName: "",
			jsonParams: `{"labelKey": "model"}`,
			wantErr:    true,
		},
		{
			name:       "malformed JSON should error",
			pluginName: "bad-json",
			jsonParams: `{"labelKey": `,
			wantErr:    true,
		},
		{
			name:       "unknown field should error with strict decoder",
			pluginName: "strict-test",
			jsonParams: `{"labelKey": "model", "unknownField": true}`,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rawParams := json.RawMessage(tt.jsonParams)
			p, err := Factory(tt.pluginName, fwkplugin.StrictDecoder(rawParams), nil)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, p)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, p)
				assert.Equal(t, PluginType, p.TypedName().Type)
				assert.Equal(t, tt.pluginName, p.TypedName().Name)
			}
		})
	}
}

func TestFilter(t *testing.T) {
	endpoints := []scheduling.Endpoint{
		createEndpoint("spoke1-llama", "10.0.0.1", map[string]string{
			"model": "llama-3-70b",
			"tier":  "gpu",
		}),
		createEndpoint("spoke2-llama", "10.0.0.2", map[string]string{
			"model": "llama-3-70b",
			"tier":  "gpu",
		}),
		createEndpoint("spoke3-mistral", "10.0.0.3", map[string]string{
			"model": "mistral-7b",
			"tier":  "gpu",
		}),
		createEndpoint("spoke4-no-label", "10.0.0.4", map[string]string{
			"tier": "cpu",
		}),
		createEndpoint("spoke5-nil-labels", "10.0.0.5", nil),
	}

	tests := []struct {
		name          string
		labelKey      string
		modelHeader   string
		targetModel   string
		headers       map[string]string
		expectedNames []string
	}{
		{
			name:          "matches endpoints via TargetModel (body fallback)",
			labelKey:      "model",
			targetModel:   "llama-3-70b",
			expectedNames: []string{"spoke1-llama", "spoke2-llama"},
		},
		{
			name:        "matches endpoints via header (IPP body-field-to-header)",
			labelKey:    "model",
			modelHeader: "x-gateway-model-name",
			targetModel: "should-be-ignored",
			headers:     map[string]string{"x-gateway-model-name": "llama-3-70b"},
			expectedNames: []string{"spoke1-llama", "spoke2-llama"},
		},
		{
			name:        "header takes priority over TargetModel",
			labelKey:    "model",
			modelHeader: "x-gateway-model-name",
			targetModel: "mistral-7b",
			headers:     map[string]string{"x-gateway-model-name": "llama-3-70b"},
			expectedNames: []string{"spoke1-llama", "spoke2-llama"},
		},
		{
			name:        "falls back to TargetModel when header is empty",
			labelKey:    "model",
			modelHeader: "x-gateway-model-name",
			targetModel: "mistral-7b",
			headers:     map[string]string{},
			expectedNames: []string{"spoke3-mistral"},
		},
		{
			name:          "matches single endpoint",
			labelKey:      "model",
			targetModel:   "mistral-7b",
			expectedNames: []string{"spoke3-mistral"},
		},
		{
			name:          "no match returns empty",
			labelKey:      "model",
			targetModel:   "gpt-4",
			expectedNames: []string{},
		},
		{
			name:          "nil request passes all through",
			labelKey:      "model",
			targetModel:   "", // signals nil-request test case
			expectedNames: []string{"spoke1-llama", "spoke2-llama", "spoke3-mistral", "spoke4-no-label", "spoke5-nil-labels"},
		},
		{
			name:          "custom label key",
			labelKey:      "tier",
			targetModel:   "gpu",
			expectedNames: []string{"spoke1-llama", "spoke2-llama", "spoke3-mistral"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := New("test", parameters{
				LabelKey:    tt.labelKey,
				ModelHeader: tt.modelHeader,
			})
			require.NoError(t, err)

			ctx := utils.NewTestContext(t)

			var request *scheduling.InferenceRequest
			if tt.targetModel != "" {
				request = &scheduling.InferenceRequest{
					TargetModel: tt.targetModel,
					Headers:     tt.headers,
				}
			}

			result := f.Filter(ctx, request, endpoints)

			actualNames := make([]string, len(result))
			for i, ep := range result {
				actualNames[i] = ep.GetMetadata().NamespacedName.Name
			}

			assert.ElementsMatch(t, tt.expectedNames, actualNames)
		})
	}
}

func TestFilterNilEndpointLabels(t *testing.T) {
	endpoints := []scheduling.Endpoint{
		createEndpoint("ep-with-label", "10.0.0.1", map[string]string{"model": "llama"}),
		createEndpoint("ep-nil-labels", "10.0.0.2", nil),
		createEndpoint("ep-empty-labels", "10.0.0.3", map[string]string{}),
	}

	f, err := New("test", parameters{LabelKey: "model"})
	require.NoError(t, err)

	ctx := utils.NewTestContext(t)
	request := &scheduling.InferenceRequest{TargetModel: "llama"}

	result := f.Filter(ctx, request, endpoints)

	require.Len(t, result, 1)
	assert.Equal(t, "ep-with-label", result[0].GetMetadata().NamespacedName.Name)
}

func TestTypedName(t *testing.T) {
	f, err := New("my-instance", parameters{LabelKey: "model"})
	require.NoError(t, err)

	tn := f.TypedName()
	assert.Equal(t, PluginType, tn.Type)
	assert.Equal(t, "my-instance", tn.Name)
}
