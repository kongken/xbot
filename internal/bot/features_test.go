package bot

import (
	"slices"
	"testing"

	telegram "github.com/go-telegram/bot"
)

type recordingRegistrar struct {
	patterns []string
}

func (r *recordingRegistrar) RegisterHandler(
	_ telegram.HandlerType,
	pattern string,
	_ telegram.MatchType,
	_ telegram.HandlerFunc,
	_ ...telegram.Middleware,
) string {
	r.patterns = append(r.patterns, pattern)
	return pattern
}

func TestRegisterFeatureHandlers(t *testing.T) {
	tests := []struct {
		name     string
		features featureSet
		patterns []string
	}{
		{
			name:     "assistant",
			features: featureSet{featureAssistant: {}},
			patterns: []string{"/gpt", "gpt", "/chat", "/sum", "/ask", "/huahua", "/save_prompt", "/hualao", "/poster"},
		},
		{
			name:     "poll",
			features: featureSet{featurePoll: {}},
			patterns: []string{"/wank", "/shit", "/sex", "/workout"},
		},
		{
			name:     "utility",
			features: featureSet{featureUtility: {}},
			patterns: []string{"/hello", "/dns_query", "/getid", "/me", "/set"},
		},
		{
			name: "composed",
			features: featureSet{
				featureAssistant: {},
				featureUtility:   {},
			},
			patterns: []string{
				"/gpt", "gpt", "/chat", "/sum", "/ask", "/huahua", "/save_prompt", "/hualao", "/poster",
				"/hello", "/dns_query", "/getid", "/me", "/set",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registrar := &recordingRegistrar{}
			registerFeatureHandlers(registrar, test.features)
			if !slices.Equal(registrar.patterns, test.patterns) {
				t.Fatalf("registered patterns = %v, want %v", registrar.patterns, test.patterns)
			}
		})
	}
}
