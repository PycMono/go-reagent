package agentprofile

// Starter is a suggested prompt shown before the first message is sent.
type Starter struct {
	Title  string
	Prompt string
}

// Skill describes one validated Profile-specific Skill.
type Skill struct {
	Name        string
	Description string
	Location    string
}

// Profile is an immutable-at-runtime chat assistant definition.
type Profile struct {
	Code         string
	Name         string
	Description  string
	Icon         string
	Order        int
	Selectable   bool
	Welcome      string
	Starters     []Starter
	Instructions string
	Skills       []Skill
}
