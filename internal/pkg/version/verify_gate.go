// This file is a deliberate lint violation to verify S0.Q1 CI gate.
// It will be reverted immediately after gate verification.
package version

import "encoding/json"

func deliberateErrcheckViolation() {
	json.Marshal(struct{}{})
}
