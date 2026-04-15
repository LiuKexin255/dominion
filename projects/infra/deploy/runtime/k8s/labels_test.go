package k8s

import (
	"testing"

	"k8s.io/apimachinery/pkg/labels"
)

func TestLabelConstants(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "managed-by", got: managedByLabelKey, want: "app.kubernetes.io/managed-by"},
		{name: "app", got: appLabelKey, want: "app.kubernetes.io/name"},
		{name: "service", got: serviceLabelKey, want: "app.kubernetes.io/component"},
		{name: "environment", got: dominionEnvironmentLabelKey, want: "dominion.io/environment"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}
}

func Test_buildLabels(t *testing.T) {
	tests := []struct {
		name    string
		options []labelOption
		want    labels.Set
	}{
		{
			name:    "nil options return nil",
			options: nil,
			want:    nil,
		},
		{
			name:    "single app label",
			options: []labelOption{withApp("grpc-hello-world")},
			want:    labels.Set{appLabelKey: "grpc-hello-world"},
		},
		{
			name: "all labels with trimming",
			options: []labelOption{
				withApp(" grpc-hello-world "),
				withService(" gateway "),
				withDominionEnvironment(" dev "),
				withManagedBy(" deploy-tool "),
			},
			want: labels.Set{
				appLabelKey:                 "grpc-hello-world",
				serviceLabelKey:             "gateway",
				dominionEnvironmentLabelKey: "dev",
				managedByLabelKey:           "deploy-tool",
			},
		},
		{
			name:    "nil option is ignored",
			options: []labelOption{nil, withService("gateway")},
			want:    labels.Set{serviceLabelKey: "gateway"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildLabels(tt.options...)
			if len(got) == 0 && len(tt.want) == 0 {
				if got != nil {
					t.Fatalf("buildLabels() = %#v, want nil", got)
				}
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("buildLabels() len = %d, want %d", len(got), len(tt.want))
			}
			for key, want := range tt.want {
				if got[key] != want {
					t.Fatalf("buildLabels()[%q] = %q, want %q", key, got[key], want)
				}
			}
		})
	}
}

func Test_hasAllLabels(t *testing.T) {
	tests := []struct {
		name    string
		current map[string]string
		want    labels.Set
		ok      bool
	}{
		{
			name:    "matches when current contains all wanted labels",
			current: map[string]string{appLabelKey: "grpc-hello-world", serviceLabelKey: "gateway"},
			want:    labels.Set{appLabelKey: "grpc-hello-world"},
			ok:      true,
		},
		{
			name:    "fails when a label is missing",
			current: map[string]string{appLabelKey: "grpc-hello-world"},
			want:    labels.Set{appLabelKey: "grpc-hello-world", serviceLabelKey: "gateway"},
			ok:      false,
		},
		{
			name:    "fails when a label value differs",
			current: map[string]string{appLabelKey: "grpc-hello-world"},
			want:    labels.Set{appLabelKey: "billing"},
			ok:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasAllLabels(tt.current, tt.want); got != tt.ok {
				t.Fatalf("hasAllLabels() = %v, want %v", got, tt.ok)
			}
		})
	}
}

func Test_buildLabelSelector(t *testing.T) {
	selector := buildLabelSelector(labels.Set{
		appLabelKey:                 "grpc-hello-world",
		serviceLabelKey:             "gateway",
		dominionEnvironmentLabelKey: "dev",
		managedByLabelKey:           "deploy-tool",
	})

	if selector != "app.kubernetes.io/component=gateway,app.kubernetes.io/managed-by=deploy-tool,app.kubernetes.io/name=grpc-hello-world,dominion.io/environment=dev" {
		t.Fatalf("buildLabelSelector() = %q", selector)
	}

	selector = buildLabelSelector(labels.Set{
		appLabelKey:     "grpc-hello-world",
		serviceLabelKey: "bad value",
	})
	if selector != "app.kubernetes.io/name=grpc-hello-world" {
		t.Fatalf("buildLabelSelector() filtered invalid values = %q", selector)
	}
}

func Test_isValidLabelValue(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{value: "", want: true},
		{value: "abc123", want: true},
		{value: "a-b_c.d", want: true},
		{value: "-abc", want: false},
		{value: "abc-", want: false},
		{value: "abc def", want: false},
		{value: "a/b", want: false},
		{value: "汉字", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			if got := isValidLabelValue(tt.value); got != tt.want {
				t.Fatalf("isValidLabelValue(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func Test_isValidLabelValueChar(t *testing.T) {
	tests := []struct {
		r    rune
		want bool
	}{
		{r: 'a', want: true},
		{r: 'Z', want: true},
		{r: '9', want: true},
		{r: '-', want: true},
		{r: '_', want: true},
		{r: '.', want: true},
		{r: '/', want: false},
		{r: ' ', want: false},
	}

	for _, tt := range tests {
		t.Run(string(tt.r), func(t *testing.T) {
			if got := isValidLabelValueChar(tt.r); got != tt.want {
				t.Fatalf("isValidLabelValueChar(%q) = %v, want %v", tt.r, got, tt.want)
			}
		})
	}
}

func Test_isASCIIAlphaNumeric(t *testing.T) {
	tests := []struct {
		r    rune
		want bool
	}{
		{r: 'a', want: true},
		{r: 'Z', want: true},
		{r: '0', want: true},
		{r: '-', want: false},
		{r: '_', want: false},
		{r: '.', want: false},
	}

	for _, tt := range tests {
		t.Run(string(tt.r), func(t *testing.T) {
			if got := isASCIIAlphaNumeric(tt.r); got != tt.want {
				t.Fatalf("isASCIIAlphaNumeric(%q) = %v, want %v", tt.r, got, tt.want)
			}
		})
	}
}
