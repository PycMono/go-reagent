package agentprofile

import agentprofileentity "github.com/PycMono/go-reagent/domain/entity/agentprofile"

// Catalog exposes the validated, versioned Agent Profiles available to chat.
type Catalog interface {
	List() []agentprofileentity.Profile
	Find(code string) (agentprofileentity.Profile, bool)
	DefaultCode() string
}
