package cmd

import (
	"net/http"
	"testing"

	"github.com/protorians/liorian-cli/internal/pkg"
)

func TestIsVersionConflict(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"409 conflict", &pkg.APIError{StatusCode: http.StatusConflict, Message: "version already published"}, true},
		{"message version already exists", &pkg.APIError{StatusCode: http.StatusBadRequest, Message: "version 0.1.0 already exists"}, true},
		{"message version conflict", &pkg.APIError{StatusCode: http.StatusBadRequest, Message: "version conflict detected"}, true},
		{"message without version", &pkg.APIError{StatusCode: http.StatusBadRequest, Message: "invalid manifest"}, false},
		{"non API error", pkg.NewError("Publication", "boom", pkg.ExitPublish), false},
		{"generic error", &pkg.APIError{StatusCode: http.StatusBadRequest, Message: "bad request"}, false},
	}
	for _, tc := range cases {
		if got := isVersionConflict(tc.err); got != tc.want {
			t.Errorf("%s: isVersionConflict(%v) = %v, want %v", tc.name, tc.err, got, tc.want)
		}
	}
}
