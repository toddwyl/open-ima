package db

import (
	"context"
	"errors"
	"testing"

	"open-ima/internal/domain/knowledgebase"
)

func TestKnowledgeBaseCreateListDelete(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	svc := knowledgebase.NewKBService(NewKnowledgeBaseRepository(database))
	ctx := context.Background()

	kb, err := svc.Create(ctx, "工作笔记", "描述")
	if err != nil || kb.ID <= 0 || kb.BizID == "" {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.Create(ctx, "工作笔记", ""); !errors.Is(err, knowledgebase.ErrNameTaken) {
		t.Fatalf("duplicate name err = %v", err)
	}
	list, err := svc.List(ctx)
	if err != nil || len(list) != 1 || list[0].ID != kb.ID || list[0].Name != "工作笔记" || list[0].MediaCount != 0 {
		t.Fatalf("list = %+v err=%v", list, err)
	}
	if _, err := database.Exec(
		`INSERT INTO medias (media_biz_id, kb_biz_id, title, source_type, source_uri, file_type) VALUES ('d1', ?, 't', 'file', 'uri', 'md')`,
		kb.BizID); err != nil {
		t.Fatal(err)
	}
	list, _ = svc.List(ctx)
	if list[0].MediaCount != 1 {
		t.Fatalf("doc count = %d", list[0].MediaCount)
	}
	if err := svc.Delete(ctx, "missing"); !errors.Is(err, knowledgebase.ErrNotFound) {
		t.Fatalf("missing kb error = %v", err)
	}
	if err := svc.Delete(ctx, kb.BizID); err != nil {
		t.Fatal(err)
	}
	if exists, _ := svc.Exists(ctx, kb.BizID); exists {
		t.Fatal("kb should be gone")
	}
}
