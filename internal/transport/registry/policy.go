package registry

import (
	"fmt"

	"go.podman.io/image/v5/signature"
)

// permissivePolicyJSON mirrors the prior go-containerregistry behavior:
// accept any image without signature verification. Sigstore/cosign wiring is
// a follow-up.
const permissivePolicyJSON = `{"default":[{"type":"insecureAcceptAnything"}]}`

// PermissivePolicyContext creates an independent context accepting any image.
// Each copy must own its context: policy evaluation mutates it and cannot run
// concurrently on the same context. The caller must call Destroy when done.
func PermissivePolicyContext() (*signature.PolicyContext, error) {
	policy, err := signature.NewPolicyFromBytes([]byte(permissivePolicyJSON))
	if err != nil {
		return nil, fmt.Errorf("parse permissive policy: %w", err)
	}
	return signature.NewPolicyContext(policy)
}
