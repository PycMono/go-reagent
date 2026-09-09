package pi

import "github.com/PycMono/go-reagent/pi/internal/workspacepolicy"

type WorkspaceWriteMode = workspacepolicy.Mode
type WorkspacePolicy = workspacepolicy.Policy

const (
	WorkspaceWriteRestricted = workspacepolicy.Restricted
	WorkspaceWriteAll        = workspacepolicy.All
)

// ValidateWorkspacePolicy validates a policy without creating its declared
// writable directories.
func ValidateWorkspacePolicy(root string, policy WorkspacePolicy) error {
	_, err := workspacepolicy.Normalize(root, policy)
	return err
}
