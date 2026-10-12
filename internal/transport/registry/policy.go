package registry

import (
	"fmt"

	"go.podman.io/image/v5/signature"
)

// permissivePolicyJSON preserves the default of accepting images without
// signature verification when no policy file is configured.
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

// PolicyContext returns a PolicyContext built from the signature verification
// policy at policyPath (a containers/image policy.json — see
// signature.Policy). An empty policyPath falls back to
// PermissivePolicyContext, preserving today's default of accepting any image
// unverified. The caller owns the context and must call Destroy when done.
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
