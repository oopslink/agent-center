package airuntime

import "context"

type Repository interface {
	GetCatalog(context.Context, string) (Catalog, error)
	CreateCLI(context.Context, CLIDefinition, int64, AuditEvent) (int64, error)
	UpdateCLI(context.Context, CLIDefinition, int64, AuditEvent) (int64, error)
	CreateModel(context.Context, ModelDefinition, int64, AuditEvent) (int64, error)
	UpdateModel(context.Context, ModelDefinition, int64, AuditEvent) (int64, error)
}

type BulkRepository interface {
	ApplyBulkImport(context.Context, BulkMutation, int64, AuditEvent) (int64, error)
}
