package orm

import "context"

type Base[Self any] Model[Self]

type Index struct{}

type IndexKey struct{}

type RowCheck struct{}

type (
	IndexesDeclarer   interface{ Indexes() []Index }
	ChecksDeclarer    interface{ Checks() []RowCheck }
	GeneratedDeclarer interface{ Generated() map[string]Expr }
)

func (Model[T]) Indexes() []Index           { return nil }
func (Model[T]) Checks() []RowCheck         { return nil }
func (Model[T]) Generated() map[string]Expr { return nil }

type (
	BeforeCreater interface {
		BeforeCreate(ctx context.Context) error
	}
	AfterCreater interface {
		AfterCreate(ctx context.Context) error
	}
	BeforeSaver interface {
		BeforeSave(ctx context.Context) error
	}
	AfterSaver interface {
		AfterSave(ctx context.Context) error
	}
	BeforeDeleter interface {
		BeforeDelete(ctx context.Context) error
	}
	AfterDeleter interface {
		AfterDelete(ctx context.Context) error
	}
	RowValidator interface {
		Validate(ctx context.Context) error
	}
)
