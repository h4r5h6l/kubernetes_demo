package task

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a task does not exist.
var ErrNotFound = errors.New("task not found")

// Repository provides persistence for tasks.
type Repository interface {
	List(ctx context.Context) ([]Task, error)
	Get(ctx context.Context, id int64) (Task, error)
	Create(ctx context.Context, in CreateInput) (Task, error)
	Update(ctx context.Context, t Task) (Task, error)
	Delete(ctx context.Context, id int64) error
}

// PostgresRepository implements Repository using PostgreSQL.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository creates a repository backed by the given pool.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

const taskColumns = `id, title, description, completed, created_at, updated_at`

func scanTask(row pgx.Row) (Task, error) {
	var t Task
	err := row.Scan(&t.ID, &t.Title, &t.Description, &t.Completed, &t.CreatedAt, &t.UpdatedAt)
	return t, err
}

// List returns all tasks, newest first.
func (r *PostgresRepository) List(ctx context.Context) ([]Task, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+taskColumns+`
		FROM tasks
		ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	tasks := make([]Task, 0)
	for rows.Next() {
		var t Task
		if err := rows.Scan(&t.ID, &t.Title, &t.Description, &t.Completed, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tasks: %w", err)
	}
	return tasks, nil
}

// Get returns one task by id.
func (r *PostgresRepository) Get(ctx context.Context, id int64) (Task, error) {
	t, err := scanTask(r.pool.QueryRow(ctx, `
		SELECT `+taskColumns+`
		FROM tasks
		WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	if err != nil {
		return Task{}, fmt.Errorf("get task %d: %w", id, err)
	}
	return t, nil
}

// Create inserts a new task and returns the stored row.
func (r *PostgresRepository) Create(ctx context.Context, in CreateInput) (Task, error) {
	t, err := scanTask(r.pool.QueryRow(ctx, `
		INSERT INTO tasks (title, description)
		VALUES ($1, $2)
		RETURNING `+taskColumns, in.Title, in.Description))
	if err != nil {
		return Task{}, fmt.Errorf("create task: %w", err)
	}
	return t, nil
}

// Update saves title, description, and completed for an existing task.
func (r *PostgresRepository) Update(ctx context.Context, t Task) (Task, error) {
	updated, err := scanTask(r.pool.QueryRow(ctx, `
		UPDATE tasks
		SET title = $2, description = $3, completed = $4, updated_at = now()
		WHERE id = $1
		RETURNING `+taskColumns, t.ID, t.Title, t.Description, t.Completed))
	if errors.Is(err, pgx.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	if err != nil {
		return Task{}, fmt.Errorf("update task %d: %w", t.ID, err)
	}
	return updated, nil
}

// Delete removes a task by id.
func (r *PostgresRepository) Delete(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM tasks WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete task %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
