// Package mysql stores reminders in MySQL.
package mysql

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/DhanushRamesh/personal-assistant/internal/remind"
	"github.com/DhanushRamesh/personal-assistant/internal/storage"
)

// row : The reminders table, as GORM sees it.
//
// Kept separate from remind.Reminder so the domain type carries no
// persistence tags. The timestamps disable GORM's automatic ones, which
// would otherwise overwrite what the domain recorded.
type row struct {
	ID          string     `gorm:"column:id;primaryKey"`
	UserID      string     `gorm:"column:user_id"`
	ClientID    *string    `gorm:"column:client_id"`
	Scope       string     `gorm:"column:scope"`
	Title       string     `gorm:"column:title"`
	Body        string     `gorm:"column:body"`
	DueAt       time.Time  `gorm:"column:due_at"`
	Repeats     *string    `gorm:"column:repeats"`
	Status      string     `gorm:"column:status"`
	CreatedAt   time.Time  `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt   time.Time  `gorm:"column:updated_at;autoUpdateTime:false"`
	LastFiredAt *time.Time `gorm:"column:last_fired_at"`
	Fires       int        `gorm:"column:fires"`
}

// TableName : Names the table this row maps to.
func (row) TableName() string { return "reminders" }

// toReminder : Converts a stored row back into a reminder.
func (r *row) toReminder() remind.Reminder {
	out := remind.Reminder{
		ID:        r.ID,
		UserID:    r.UserID,
		ClientID:  value(r.ClientID),
		Scope:     remind.Scope(r.Scope),
		Title:     r.Title,
		Body:      r.Body,
		DueAt:     r.DueAt.UTC(),
		Repeats:   remind.Repeat(value(r.Repeats)),
		Status:    remind.Status(r.Status),
		CreatedAt: r.CreatedAt.UTC(),
		UpdatedAt: r.UpdatedAt.UTC(),
		Fires:     r.Fires,
	}
	if r.LastFiredAt != nil {
		at := r.LastFiredAt.UTC()
		out.LastFiredAt = &at
	}
	return out
}

// toRow : Converts a reminder into the row that stores it.
func toRow(r *remind.Reminder) *row {
	return &row{
		ID:          r.ID,
		UserID:      r.UserID,
		ClientID:    nullable(r.ClientID),
		Scope:       string(r.Scope),
		Title:       r.Title,
		Body:        r.Body,
		DueAt:       r.DueAt.UTC(),
		Repeats:     nullable(string(r.Repeats)),
		Status:      string(r.Status),
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
		LastFiredAt: r.LastFiredAt,
		Fires:       r.Fires,
	}
}

// nullable : An empty string as a null column.
func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// value : A null column as an empty string.
func value(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Store : A remind.Store backed by MySQL.
type Store struct {
	db *gorm.DB
}

// New : Returns a Store reading and writing through db.
func New(db *storage.DB) *Store { return &Store{db: db.DB} }

// Create : Stores a new reminder.
func (s *Store) Create(ctx context.Context, r *remind.Reminder) error {
	if err := r.Valid(); err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Create(toRow(r)).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return fmt.Errorf("remind: %s already exists", r.ID)
		}
		return fmt.Errorf("remind: creating %s: %w", r.ID, err)
	}
	return nil
}

// Get : Returns one reminder, or remind.ErrNotFound.
func (s *Store) Get(ctx context.Context, userID, id string) (*remind.Reminder, error) {
	var got row
	err := s.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", id, userID).
		Take(&got).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, remind.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("remind: reading %s: %w", id, err)
	}
	out := got.toReminder()
	return &out, nil
}

// List : A person's reminders in the given states, soonest first.
func (s *Store) List(ctx context.Context, userID string, states ...remind.Status) ([]remind.Reminder, error) {
	query := s.db.WithContext(ctx).Model(&row{}).Where("user_id = ?", userID)
	if len(states) > 0 {
		wanted := make([]string, 0, len(states))
		for _, state := range states {
			wanted = append(wanted, string(state))
		}
		query = query.Where("status IN ?", wanted)
	}

	var rows []row
	if err := query.Order("due_at ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("remind: listing: %w", err)
	}
	return toReminders(rows), nil
}

// Cancel : Calls one off. Cancelling one that has already happened is not
// an error: the caller wanted it not to happen, and it will not.
func (s *Store) Cancel(ctx context.Context, userID, id string) error {
	out := s.db.WithContext(ctx).Model(&row{}).
		Where("id = ? AND user_id = ?", id, userID).
		Updates(map[string]any{
			"status":     string(remind.Cancelled),
			"updated_at": time.Now().UTC(),
		})
	if out.Error != nil {
		return fmt.Errorf("remind: cancelling %s: %w", id, out.Error)
	}
	if out.RowsAffected == 0 {
		return remind.ErrNotFound
	}
	return nil
}

// Due : Pending reminders whose time has come, soonest first.
func (s *Store) Due(ctx context.Context, at time.Time, limit int) ([]remind.Reminder, error) {
	if limit <= 0 {
		limit = remind.DefaultDueLimit
	}

	var rows []row
	err := s.db.WithContext(ctx).
		Where("status = ? AND due_at <= ?", string(remind.Pending), at.UTC()).
		Order("due_at ASC, id ASC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("remind: reading what is due: %w", err)
	}
	return toReminders(rows), nil
}

// Fired : Records that a reminder was said. A zero next finishes it.
func (s *Store) Fired(ctx context.Context, id string, at, next time.Time) error {
	changes := map[string]any{
		"last_fired_at": at.UTC(),
		"fires":         gorm.Expr("fires + 1"),
		"updated_at":    at.UTC(),
	}
	if next.IsZero() {
		changes["status"] = string(remind.Done)
	} else {
		changes["due_at"] = next.UTC()
	}

	// Only while it is still pending, so two passes of the firing loop
	// cannot both claim it.
	out := s.db.WithContext(ctx).Model(&row{}).
		Where("id = ? AND status = ?", id, string(remind.Pending)).
		Updates(changes)
	if out.Error != nil {
		return fmt.Errorf("remind: recording that %s fired: %w", id, out.Error)
	}
	if out.RowsAffected == 0 {
		return remind.ErrNotFound
	}
	return nil
}

// Missed : Records that a reminder's time passed with nothing listening.
func (s *Store) Missed(ctx context.Context, id string, at time.Time) error {
	out := s.db.WithContext(ctx).Model(&row{}).
		Where("id = ? AND status = ?", id, string(remind.Pending)).
		Updates(map[string]any{
			"status":     string(remind.Missed),
			"updated_at": at.UTC(),
		})
	if out.Error != nil {
		return fmt.Errorf("remind: recording that %s was missed: %w", id, out.Error)
	}
	if out.RowsAffected == 0 {
		return remind.ErrNotFound
	}
	return nil
}

// Reschedule : Moves a reminder to its next time without saying it.
func (s *Store) Reschedule(ctx context.Context, id string, next time.Time) error {
	out := s.db.WithContext(ctx).Model(&row{}).
		Where("id = ? AND status = ?", id, string(remind.Pending)).
		Updates(map[string]any{
			"due_at":     next.UTC(),
			"updated_at": time.Now().UTC(),
		})
	if out.Error != nil {
		return fmt.Errorf("remind: rescheduling %s: %w", id, out.Error)
	}
	if out.RowsAffected == 0 {
		return remind.ErrNotFound
	}
	return nil
}

// toReminders : Converts stored rows back into reminders.
func toReminders(rows []row) []remind.Reminder {
	out := make([]remind.Reminder, 0, len(rows))
	for i := range rows {
		out = append(out, rows[i].toReminder())
	}
	return out
}

// Ensure the store satisfies the interface it exists to provide.
var _ remind.Store = (*Store)(nil)
