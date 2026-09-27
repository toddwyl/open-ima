// Package dao 承载元数据库的行级 SQL 操作:仅依赖标准库,不感知领域模型。
// 行到领域实体的映射与错误翻译(ErrNoRows、UNIQUE 冲突等)由上层 Repository 完成。
package dao

// scanner 抽象 *sql.Row 与 *sql.Rows,便于单行与多行查询共用扫描逻辑。
type scanner interface{ Scan(dest ...any) error }
