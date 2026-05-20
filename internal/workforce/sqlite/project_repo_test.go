package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/workforce"
)

func TestProjectRepo_SaveAndFindByID(t *testing.T) {
	db := openTestDB(t)
	repo := NewProjectRepo(db)
	p := newProject(t, "agent-center")
	if err := repo.Save(context.Background(), p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := repo.FindByID(context.Background(), "agent-center")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.ID() != "agent-center" {
		t.Fatal()
	}
	if got.Kind() != workforce.ProjectKindCoding {
		t.Fatal()
	}
	if got.Version() != 1 {
		t.Fatal()
	}
}

func TestProjectRepo_Save_Duplicate(t *testing.T) {
	db := openTestDB(t)
	repo := NewProjectRepo(db)
	_ = repo.Save(context.Background(), newProject(t, "p"))
	err := repo.Save(context.Background(), newProject(t, "p"))
	if !errors.Is(err, workforce.ErrProjectAlreadyExists) {
		t.Fatalf("got %v", err)
	}
}

func TestProjectRepo_FindByID_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := NewProjectRepo(db)
	_, err := repo.FindByID(context.Background(), "nope")
	if !errors.Is(err, workforce.ErrProjectNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestProjectRepo_Update_Happy(t *testing.T) {
	db := openTestDB(t)
	repo := NewProjectRepo(db)
	_ = repo.Save(context.Background(), newProject(t, "p"))
	newName := "Renamed"
	got, err := repo.Update(context.Background(), "p",
		workforce.ProjectUpdateFields{Name: &newName}, 1, time.Now())
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.Name() != "Renamed" {
		t.Fatal()
	}
	if got.Version() != 2 {
		t.Fatalf("version: %d", got.Version())
	}
}

func TestProjectRepo_Update_VersionConflict(t *testing.T) {
	db := openTestDB(t)
	repo := NewProjectRepo(db)
	_ = repo.Save(context.Background(), newProject(t, "p"))
	newName := "Renamed"
	_, err := repo.Update(context.Background(), "p",
		workforce.ProjectUpdateFields{Name: &newName}, 99, time.Now())
	if !errors.Is(err, workforce.ErrProjectVersionConflict) {
		t.Fatalf("got %v", err)
	}
}

func TestProjectRepo_Update_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := NewProjectRepo(db)
	n := "x"
	_, err := repo.Update(context.Background(), "nope",
		workforce.ProjectUpdateFields{Name: &n}, 1, time.Now())
	if !errors.Is(err, workforce.ErrProjectNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestProjectRepo_Update_NoChanges(t *testing.T) {
	db := openTestDB(t)
	repo := NewProjectRepo(db)
	_ = repo.Save(context.Background(), newProject(t, "p"))
	_, err := repo.Update(context.Background(), "p",
		workforce.ProjectUpdateFields{}, 1, time.Now())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestProjectRepo_Delete_Happy(t *testing.T) {
	db := openTestDB(t)
	repo := NewProjectRepo(db)
	_ = repo.Save(context.Background(), newProject(t, "p"))
	if err := repo.Delete(context.Background(), "p"); err != nil {
		t.Fatal(err)
	}
	_, err := repo.FindByID(context.Background(), "p")
	if !errors.Is(err, workforce.ErrProjectNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestProjectRepo_Delete_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := NewProjectRepo(db)
	err := repo.Delete(context.Background(), "nope")
	if !errors.Is(err, workforce.ErrProjectNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestProjectRepo_Delete_HasActiveMapping(t *testing.T) {
	db := openTestDB(t)
	projRepo := NewProjectRepo(db)
	workerRepo := NewWorkerRepo(db)
	mapRepo := NewMappingRepo(db)
	_ = workerRepo.Save(context.Background(), newWorker(t, "W-1"))
	_ = projRepo.Save(context.Background(), newProject(t, "p"))
	m, _ := workforce.NewWorkerProjectMapping(workforce.NewMappingInput{
		ID: workforce.MappingID(idgen.MustNewULID()),
		WorkerID: "W-1", ProjectID: "p", BasePath: "/x", AddedAt: time.Now(),
	})
	_ = mapRepo.Save(context.Background(), m)
	err := projRepo.Delete(context.Background(), "p")
	if !errors.Is(err, workforce.ErrProjectHasActiveDeps) {
		t.Fatalf("got %v", err)
	}
}

func TestProjectRepo_FindAll(t *testing.T) {
	db := openTestDB(t)
	repo := NewProjectRepo(db)
	for _, id := range []workforce.ProjectID{"a-proj", "b-proj"} {
		_ = repo.Save(context.Background(), newProject(t, id))
	}
	got, err := repo.FindAll(context.Background(), workforce.ProjectFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("FindAll: %d", len(got))
	}
}

func TestProjectRepo_FindAll_FilterByKind(t *testing.T) {
	db := openTestDB(t)
	repo := NewProjectRepo(db)
	for _, id := range []workforce.ProjectID{"a-proj", "b-proj"} {
		_ = repo.Save(context.Background(), newProject(t, id))
	}
	wp, _ := workforce.NewProject(workforce.NewProjectInput{
		ID: "writing-proj", Name: "W", Kind: workforce.ProjectKindWriting,
		CreatedByIdentityID: "user:x", CreatedAt: time.Now(),
	})
	_ = repo.Save(context.Background(), wp)
	kind := workforce.ProjectKindCoding
	got, _ := repo.FindAll(context.Background(), workforce.ProjectFilter{Kind: &kind})
	if len(got) != 2 {
		t.Fatalf("expected 2 coding projects, got %d", len(got))
	}
}

func TestProjectRepo_Save_NilProject(t *testing.T) {
	db := openTestDB(t)
	repo := NewProjectRepo(db)
	if err := repo.Save(context.Background(), nil); err == nil {
		t.Fatal()
	}
}
