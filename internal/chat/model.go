package chat

import "strings"

// Model : Which model answers a client's prompts.
//
// The zero value means the client has not chosen one and the server's
// configured model answers instead. That is what every client starts as, so
// choosing nothing keeps working the way it did.
type Model struct {
	// Vendor : Who makes it. Part of the identity, because an identifier is
	// only unique within a vendor.
	Vendor string
	// ID : What the service calls it.
	ID string
}

// Chosen : Whether a model has been picked, as against left to the server.
func (m Model) Chosen() bool { return m.ID != "" }

// ServerDefault : The zero Model, meaning whatever the server is configured
// with. Named so a caller clearing a choice says what it means.
var ServerDefault = Model{}

// NewModel : A model choice with its surrounding whitespace removed.
//
// An empty identifier gives ServerDefault whatever the vendor says, since a
// vendor alone does not name anything to call.
func NewModel(vendor, id string) Model {
	id = strings.TrimSpace(id)
	if id == "" {
		return ServerDefault
	}
	return Model{Vendor: strings.TrimSpace(vendor), ID: id}
}
