package registry

import (
	"fmt"
	"sync"

	"go.podman.io/image/v5/signature"
)

// permissivePolicyJSON mirrors the prior go-containerregistry behavior:
// accept any image without signature verification. Sigstore/cosign wiring is
// a follow-up.
const permissivePolicyJSON = `{"default":[{"type":"insecureAcceptAnything"}]}`

var (
	policyOnce sync.Once
	policyCtx  *signature.PolicyContext
	policyErr  error
)

// PermissivePolicyContext returns a process-wide PolicyContext that accepts
// any image. Safe for concurrent use.
func PermissivePolicyContext() (*signature.PolicyContext, error) {
	policyOnce.Do(func() {
		policy, err := signature.NewPolicyFromBytes([]byte(permissivePolicyJSON))
		if err != nil {
			policyErr = fmt.Errorf("parse permissive policy: %w", err)
			return
		}
		policyCtx, policyErr = signature.NewPolicyContext(policy)
	})
	return policyCtx, policyErr
}
