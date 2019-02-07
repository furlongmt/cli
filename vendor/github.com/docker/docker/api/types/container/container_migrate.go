package container

// MATT ADDED THIS
type CreatePageServerBody struct {
	Port uint32

	// Warnings encountered when creating the page esrver
	// Required: true
	Warnings []string `json:"Warnings"`
}
