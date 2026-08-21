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

// PolicyContext returns a PolicyContext built from the signature verification
// policy at policyPath (a containers/image policy.json — see
// signature.Policy). An empty policyPath falls back to
// PermissivePolicyContext, preserving today's default of accepting any image
// unverified.
func PolicyContext(policyPath string) (*signature.PolicyContext, error) {
	if policyPath == "" {
		return PermissivePolicyContext()
	}
	policy, err := signature.NewPolicyFromFile(policyPath)
	if err != nil {
		return nil, fmt.Errorf("load signature policy %q: %w", policyPath, err)
	}
	policyCtx, err := signature.NewPolicyContext(policy)
	if err != nil {
		return nil, fmt.Errorf("build policy context from %q: %w", policyPath, err)
	}
	return policyCtx, nil
}
