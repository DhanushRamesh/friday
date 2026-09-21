package task

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
)

const (
	// DeviceIDPrefix : Marks an identifier as belonging to a device.
	DeviceIDPrefix = "dev_"
	// deviceIDLen : The length of a prefixed device identifier.
	deviceIDLen = len(DeviceIDPrefix) + ulid.EncodedSize
	// MaxDeviceNameRunes : The longest name a device may be given. It is a
	// label for a person reading a list of their devices, not a field to hold
	// data in.
	MaxDeviceNameRunes = 100
)

// ErrDeviceNameTooLong : The name exceeded MaxDeviceNameRunes.
var ErrDeviceNameTooLong = errors.New("task: device name is too long")

// Device : One thing a user talks to FRIDAY through, such as a phone, a
// laptop or a speaker.
//
// A device is a credential and nothing more: it owns no conversations and is
// not a boundary between anyone. Its user is. Several exist per user so that
// a lost phone is one revocation rather than a password change.
//
// Each device holds its own active conversation, because a person may be
// speaking to a speaker in one room while typing at a laptop in another.
// Which conversations exist is a property of the user; which one a device is
// currently in is a property of the device.
type Device struct {
	// ID : The identifier, a DeviceIDPrefix followed by a ULID.
	ID string
	// UserID : Whose device it is.
	UserID string
	// Name : What to call it in a listing. May be empty.
	Name string
	// TokenHash : The stored form of the token this device authenticates
	// with. The token itself exists only at the moment of logging in.
	TokenHash string
	// RevokedAt : When the device was revoked, or nil while it is usable.
	RevokedAt *time.Time
	// ActiveConversationID : Where a prompt from this device lands. Empty
	// only before the first conversation is created.
	ActiveConversationID string

	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewDevice : Creates a device belonging to a user, authenticating with the
// given token hash. The name is optional and is trimmed.
func NewDevice(userID, name, tokenHash string) (*Device, error) {
	if userID == "" {
		return nil, errors.New("task: a device must belong to a user")
	}
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) > MaxDeviceNameRunes {
		return nil, ErrDeviceNameTooLong
	}

	registered := now()
	return &Device{
		ID:        NewDeviceID(),
		UserID:    userID,
		Name:      name,
		TokenHash: tokenHash,
		CreatedAt: registered,
		UpdatedAt: registered,
	}, nil
}

// Revoked : Reports whether the device may no longer authenticate.
func (d *Device) Revoked() bool { return d.RevokedAt != nil }

// NewDeviceID : Returns a fresh device identifier.
func NewDeviceID() string { return DeviceIDPrefix + ulid.Make().String() }

// ValidDeviceID : Reports whether id is shaped like a device identifier. It
// checks the form only; no such device need exist.
func ValidDeviceID(id string) bool {
	if len(id) != deviceIDLen || !strings.HasPrefix(id, DeviceIDPrefix) {
		return false
	}
	_, err := ulid.ParseStrict(strings.TrimPrefix(id, DeviceIDPrefix))
	return err == nil
}
