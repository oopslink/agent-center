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
	p := newProject(t, "proj-aabb0001")
	if err := repo.Save(context.Background(), p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := repo.FindByID(context.Background(), "proj-aabb0001")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.ID() != "proj-aabb0001" {
		t.Fatal()
	}
	tags := got.Tags()
	if len(tags) != 1 || tags[0] != "coding" {
		t.Fatalf("tags = %v", tags)
	}
	if got.Version() != 1 {
		t.Fatal()
	}
}

func TestProjectRepo_Save_Duplicate(t *testing.T) {
	db := openTestDB(t)
	repo := NewProjectRepo(db)
	_ = repo.Save(context.Background(), newProject(t, "proj-aabb0002"))
	err := repo.Save(context.Background(), newProject(t, "proj-aabb0002"))
	if !errors.Is(err, workforce.ErrProjectAlreadyExists) {
		t.Fatalf("got %v", err)
	}
}

func TestProjectRepo_FindByID_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := NewProjectRepo(db)
	_, err := repo.FindByID(context.Background(), "proj-99999999")
	if !errors.Is(err, workforce.ErrProjectNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestProjectRepo_Update_Happy(t *testing.T) {
	db := openTestDB(t)
	repo := NewProjectRepo(db)
	_ = repo.Save(context.Background(), newProject(t, "proj-aabb0003"))
	newName := "Renamed"
	got, err := repo.Update(context.Background(), "proj-aabb0003",
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

func TestProjectRepo_Update_Tags(t *testing.T) {
	db := openTestDB(t)
	repo := NewProjectRepo(db)
	_ = repo.Save(context.Background(), newProject(t, "proj-aabb0004"))
	newTags := []string{"ops", "docs"}
	got, err := repo.Update(context.Background(), "proj-aabb0004",
		workforce.ProjectUpdateFields{Tags: &newTags}, 1, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	tags := got.Tags()
	if len(tags) != 2 || tags[0] != "ops" || tags[1] != "docs" {
		t.Fatalf("tags = %v", tags)
	}
}

func TestProjectRepo_Update_VersionConflict(t *testing.T) {
	db := openTestDB(t)
	repo := NewProjectRepo(db)
	_ = repo.Save(context.Background(), newProject(t, "proj-aabb0005"))
	newName := "Renamed"
	_, err := repo.Update(context.Background(), "proj-aabb0005",
		workforce.ProjectUpdateFields{Name: &newName}, 99, time.Now())
	if !errors.Is(err, workforce.ErrProjectVersionConflict) {
		t.Fatalf("got %v", err)
	}
}

func TestProjectRepo_Update_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := NewProjectRepo(db)
	n := "x"
	_, err := repo.Update(context.Background(), "proj-99999999",
		workforce.ProjectUpdateFields{Name: &n}, 1, time.Now())
	if !errors.Is(err, workforce.ErrProjectNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestProjectRepo_Update_NoChanges(t *testing.T) {
	db := openTestDB(t)
	repo := NewProjectRepo(db)
	_ = repo.Save(context.Background(), newProject(t, "proj-aabb0006"))
	_, err := repo.Update(context.Background(), "proj-aabb0006",
		workforce.ProjectUpdateFields{}, 1, time.Now())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestProjectRepo_Delete_Happy(t *testing.T) {
	db := openTestDB(t)
	repo := NewProjectRepo(db)
	_ = repo.Save(context.Background(), newProject(t, "proj-aabb0007"))
	if err := repo.Delete(context.Background(), "proj-aabb0007"); err != nil {
		t.Fatal(err)
	}
	_, err := repo.FindByID(context.Background(), "proj-aabb0007")
	if !errors.Is(err, workforce.ErrProjectNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestProjectRepo_Delete_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := NewProjectRepo(db)
	err := repo.Delete(context.Background(), "proj-99999999")
	if !errors.Is(err, workforce.ErrProjectNotFound) {
		t.Fatalf("got %v", err)
	}
}

// conventions § 9.w: schema declares no FOREIGN KEY. Repository.Delete is
// a thin DB delete; the "has active mappings" precondition is enforced by
// ProjectCRUDService.Remove (see workforce/service package). The
// repository simply deletes the row even when mappings still reference it.
func TestProjectRepo_Delete_RowOnly_NoFKPrecondition(t *testing.T) {
	db := openTestDB(t)
	projRepo := NewProjectRepo(db)
	workerRepo := NewWorkerRepo(db)
	mapRepo := NewMappingRepo(db)
	_ = workerRepo.Save(context.Background(), newWorker(t, "W-1"))
	_ = projRepo.Save(context.Background(), newProject(t, "proj-aabb0008"))
	m, _ := workforce.NewWorkerProjectMapping(workforce.NewMappingInput{
		ID:       workforce.MappingID(idgen.MustNewULID()),
		WorkerID: "W-1", ProjectID: "proj-aabb0008", BasePath: "/x", AddedAt: time.Now(),
	})
	_ = mapRepo.Save(context.Background(), m)
	if err := projRepo.Delete(context.Background(), "proj-aabb0008"); err != nil {
		t.Fatalf("Delete should succeed without FK: %v", err)
	}
	if _, err := projRepo.FindByID(context.Background(), "proj-aabb0008"); !errors.Is(err, workforce.ErrProjectNotFound) {
		t.Fatalf("project should be gone: %v", err)
	}
}

func TestProjectRepo_FindAll(t *testing.T) {
	db := openTestDB(t)
	repo := NewProjectRepo(db)
	for _, id := range []workforce.ProjectID{"proj-aabbcc01", "proj-aabbcc02"} {
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

func TestProjectRepo_Save_NilProject(t *testing.T) {
	db := openTestDB(t)
	repo := NewProjectRepo(db)
	if err := repo.Save(context.Background(), nil); err == nil {
		t.Fatal()
	}
}
