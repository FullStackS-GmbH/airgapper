package registry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManifestExists(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name       string
		status     int
		wantExists bool
		wantErr    bool
	}{
		{name: "present", status: http.StatusOK, wantExists: true},
		{name: "absent", status: http.StatusNotFound},
		{name: "server error is not absence", status: http.StatusInternalServerError, wantErr: true},
		{name: "forbidden is not absence", status: http.StatusForbidden, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v2/" {
					return
				}
				if tt.status == http.StatusOK {
					w.Header().Set("Docker-Content-Digest", "sha256:"+"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
				}
				w.WriteHeader(tt.status)
			}))
			defer srv.Close()

			ref, err := ParseRef(srv.Listener.Addr().String() + "/repo/app:v1")
			require.NoError(t, err)
			exists, err := ManifestExists(context.Background(), SystemContext(nil, true, ""), ref, nil)
			assert.Equal(t, tt.wantExists, exists)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
