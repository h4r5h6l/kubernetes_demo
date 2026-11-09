package task

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/h4r5h6l/kubernetes_demo/internal/database"
	"github.com/h4r5h6l/kubernetes_demo/migrations"
)

// TestPostgresRepositoryCRUD is an opt-in integration test. Set
// TEST_DATABASE_URL to a disposable PostgreSQL database to run it.
func TestPostgresRepositoryCRUD(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL integration tests")
	}

	ctx := context.Background()
	pool, err := database.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	if err := database.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	repo := NewPostgresRepository(pool)

	created, err := repo.Create(ctx, CreateInput{
		Title:       "integration test",
		Description: "created by repository test",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() {
		// Best effort: the task may already be deleted by the test.
		_ = repo.Delete(context.Background(), created.ID)
	})

	if created.ID == 0 {
		t.Fatal("created.ID = 0, want non-zero")
	}
	if created.Title != "integration test" || created.Completed {
		t.Errorf("created = %+v, want stored title and completed=false", created)
	}

	got, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("got.ID = %d, want %d", got.ID, created.ID)
	}

	updated, err := repo.Update(ctx, Task{
		ID:          got.ID,
		Title:       "updated title",
		Description: got.Description,
		Completed:   true,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Title != "updated title" || !updated.Completed {
		t.Errorf("updated = %+v, want updated fields", updated)
	}

	list, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	found := false
	for _, item := range list {
		if item.ID == got.ID {
			found = true
			break
		}
	}
	if !found {
		t.Error("created task not found in List()")
	}

	if err := repo.Delete(ctx, got.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.Get(ctx, got.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after delete error = %v, want ErrNotFound", err)
	}
	if err := repo.Delete(ctx, got.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second Delete error = %v, want ErrNotFound", err)
	}
}
