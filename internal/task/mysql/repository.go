// Package mysql : Stores tasks in MySQL.
//
// It holds the only knowledge of how a task becomes a row. The domain type in
// internal/task carries no persistence tags, so every mapping between the two
// happens here.
package mysql

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/DhanushRamesh/friday/internal/storage"
	"github.com/DhanushRamesh/friday/internal/task"
)

// Repository : A task.Repository backed by MySQL.
type Repository struct {
	db *gorm.DB
}

// NewRepository : Returns a Repository reading and writing through db.
func NewRepository(db *storage.DB) *Repository {
	return &Repository{db: db.DB}
}

// Create : Stores a new task.
func (r *Repository) Create(ctx context.Context, t *task.Task) error {
	if err := r.db.WithContext(ctx).Create(toRow(t)).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return fmt.Errorf("task: %s already exists", t.ID)
		}
		return fmt.Errorf("task: creating %s: %w", t.ID, err)
	}
	return nil
}

// Get : Returns the task with the given identifier, including its response.
func (r *Repository) Get(ctx context.Context, id string) (*task.Task, error) {
	var row taskRow
	err := r.db.WithContext(ctx).First(&row, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, task.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("task: reading %s: %w", id, err)
	}
	return row.toTask(), nil
}

// Update : Writes a task's current state over the stored one.
//
// Every column is written rather than only those GORM considers changed,
// because a field returning to its zero value is a real change: a response
// cleared, or a task moving back to running.
func (r *Repository) Update(ctx context.Context, t *task.Task) error {
	row := toRow(t)

	result := r.db.WithContext(ctx).
		Model(&taskRow{}).
		Where("id = ?", t.ID).
		Select("prompt", "status", "response", "error",
			"created_at", "updated_at", "started_at", "finished_at").
		Updates(row)

	if result.Error != nil {
		return fmt.Errorf("task: updating %s: %w", t.ID, result.Error)
	}
	if result.RowsAffected == 0 {
		// No rows changed means either the task is gone or nothing differed.
		// Only the first is an error, so check which.
		var exists int64
		if err := r.db.WithContext(ctx).Model(&taskRow{}).
			Where("id = ?", t.ID).Count(&exists).Error; err != nil {
			return fmt.Errorf("task: updating %s: %w", t.ID, err)
		}
		if exists == 0 {
			return task.ErrNotFound
		}
	}
	return nil
}

// List : Returns tasks newest first, without their responses.
func (r *Repository) List(ctx context.Context, f task.Filter) ([]task.Summary, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = task.DefaultListLimit
	}
	if limit > task.MaxListLimit {
		limit = task.MaxListLimit
	}

	query := r.db.WithContext(ctx).
		Model(&taskRow{}).
		Select(summaryColumns).
		// Identifiers are ULIDs, so ordering by the primary key orders by
		// creation time without a sort.
		Order("id DESC").
		Limit(limit)

	if f.Status != "" {
		query = query.Where("status = ?", string(f.Status))
	}

	var rows []summaryRow
	if err := query.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("task: listing: %w", err)
	}

	summaries := make([]task.Summary, len(rows))
	for i := range rows {
		summaries[i] = rows[i].toSummary()
	}
	return summaries, nil
}

// AppendMessage : Records a message against a task, assigning it the next
// position in that task's stream.
//
// The position is computed in the insert rather than read first and written
// after, so two appends cannot settle on the same number. Were they to race,
// the primary key on (task_id, seq) rejects the second.
func (r *Repository) AppendMessage(ctx context.Context, taskID, kind, text string) (task.Message, error) {
	now := time.Now().UTC().Truncate(task.StoredPrecision)

	err := r.db.WithContext(ctx).Exec(
		"INSERT INTO task_messages (task_id, seq, kind, `text`, created_at) "+
			"SELECT ?, COALESCE(MAX(seq), 0) + 1, ?, ?, ? FROM task_messages WHERE task_id = ?",
		taskID, kind, text, now, taskID,
	).Error
	if err != nil {
		// The foreign key rejects a message for a task that does not exist.
		if errors.Is(err, gorm.ErrForeignKeyViolated) {
			return task.Message{}, task.ErrNotFound
		}
		return task.Message{}, fmt.Errorf("task: appending message to %s: %w", taskID, err)
	}

	var row messageRow
	if err := r.db.WithContext(ctx).
		Where("task_id = ?", taskID).
		Order("seq DESC").
		First(&row).Error; err != nil {
		return task.Message{}, fmt.Errorf("task: reading back message for %s: %w", taskID, err)
	}
	return row.toMessage(), nil
}

// Messages : Returns a task's messages in the order they were produced.
func (r *Repository) Messages(ctx context.Context, taskID string) ([]task.Message, error) {
	var rows []messageRow
	err := r.db.WithContext(ctx).
		Where("task_id = ?", taskID).
		Order("seq ASC").
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("task: reading messages for %s: %w", taskID, err)
	}

	messages := make([]task.Message, len(rows))
	for i := range rows {
		messages[i] = rows[i].toMessage()
	}
	return messages, nil
}

// FailRunning : Marks every task still recorded as running as failed.
func (r *Repository) FailRunning(ctx context.Context, reason string) (int64, error) {
	now := time.Now().UTC().Truncate(task.StoredPrecision)

	result := r.db.WithContext(ctx).
		Model(&taskRow{}).
		Where("status = ?", string(task.StatusRunning)).
		Updates(map[string]any{
			"status":      string(task.StatusFailed),
			"error":       reason,
			"updated_at":  now,
			"finished_at": now,
		})
	if result.Error != nil {
		return 0, fmt.Errorf("task: failing interrupted tasks: %w", result.Error)
	}
	return result.RowsAffected, nil
}

// Repository implements the interface the rest of FRIDAY depends on.
var _ task.Repository = (*Repository)(nil)
