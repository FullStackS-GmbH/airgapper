package registry

import (
	"fmt"

	"go.podman.io/image/v5/signature"
)

// permissivePolicyJSON mirrors the prior go-containerregistry behavior:
// accept any image without signature verification. Sigstore/cosign wiring is
// a follow-up.
const permissivePolicyJSON = `{"default":[{"type":"insecureAcceptAnything"}]}`

var permissivePolicy, permissivePolicyErr = newPermissivePolicy()

func newPermissivePolicy() (*signature.PolicyContext, error) {
	policy, err := signature.NewPolicyFromBytes([]byte(permissivePolicyJSON))
	if err != nil {
		return nil, fmt.Errorf("parse permissive policy: %w", err)
	}
	return signature.NewPolicyContext(policy)
}

// PermissivePolicyContext returns a process-wide PolicyContext that accepts
// any image. Safe for concurrent use.
func PermissivePolicyContext() (*signature.PolicyContext, error) {
	return permissivePolicy, permissivePolicyErr
}
