package dbman

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/lib/pq"
)

type ColumnSchema struct {
	Name  string
	Type  string
	Attrs []string
}

type TableSchema struct {
	Name    string
	Columns []ColumnSchema
}

type querier interface {
	PingContext(context.Context) error
	Query(string, ...interface{}) (*sql.Rows, error)
	Stats() sql.DBStats
	Close() error
}

type metaQuerier interface {
	querier
	ListTables() ([]string, error)
	ListTablesInSchema(string) ([]string, error)
	ListSchemas() ([]string, error)
	DescribeTable(string) (*TableSchema, error)
}

func postgresOpen(conn *Connection) (*sql.DB, error) {
	sslmode, ok := conn.DriverOpts["sslmode"]
	if !ok {
		sslmode = "require"
	}
	dsn := fmt.Sprintf("host=%s port=%d dbname=%s user=%s password=%s sslmode=%s",
		conn.Host,
		conn.Port,
		conn.Database,
		conn.Username,
		conn.Password,
		sslmode,
	)
	connector, err := pq.NewConnector(dsn)
	if err != nil {
		return nil, err
	}
	db := sql.OpenDB(connector)
	return db, nil
}

type dbMeta struct {
	querier
}

func (m dbMeta) ListTables() ([]string, error) {
	return m.ListTablesInSchema("public")
}

func (m dbMeta) ListTablesInSchema(schema string) ([]string, error) {
	rows, err := m.Query(`SELECT table_name FROM information_schema.tables WHERE table_schema = $1 AND table_type = 'BASE TABLE' ORDER BY table_name`, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		tables = append(tables, name)
	}

	return tables, rows.Err()
}

var ignoreSchemas = []string{
	"pg_*",
	"information_schema",
}

func isIgnoredSchema(name string) bool {
	for _, ign := range ignoreSchemas {
		if ign == name {
			return true
		} else if strings.HasSuffix(ign, "*") {
			if strings.HasPrefix(name, ign[:len(ign)-1]) {
				return true
			}
		}
	}
	return false
}

func (m dbMeta) ListSchemas() ([]string, error) {
	rows, err := m.Query(`SELECT schema_name FROM information_schema.schemata`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var schemas []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		if !isIgnoredSchema(name) {
			schemas = append(schemas, name)
		}
	}

	return schemas, nil
}

func (m dbMeta) DescribeTable(tablename string) (*TableSchema, error) {
	var (
		schema string
		table  string
	)
	parts := strings.Split(tablename, ".")
	switch len(parts) {
	case 2:
		schema = parts[0]
		table = parts[1]

	case 1:
		schema = "public"
		table = parts[0]

	default:
		return nil, fmt.Errorf("invalid table name: '%s'", tablename)
	}

	rows, err := m.Query(`SELECT column_name, data_type, column_default, is_nullable, udt_name FROM information_schema.columns WHERE table_schema = $1 AND table_name = $2 ORDER BY ordinal_position`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := TableSchema{
		Name: table,
	}
	for rows.Next() {
		var col ColumnSchema

		var (
			defaultVal *string
			nullable   string
			userType   string
		)
		if err := rows.Scan(&col.Name, &col.Type, &defaultVal, &nullable, &userType); err != nil {
			return nil, err
		}

		if col.Type == "USER-DEFINED" {
			col.Type = userType
		}

		if defaultVal != nil {
			col.Attrs = append(col.Attrs, "DEFAULT "+*defaultVal)
		}

		if nullable == "YES" {
			col.Attrs = append(col.Attrs, "NULL")
		} else {
			col.Attrs = append(col.Attrs, "NOT NULL")
		}

		result.Columns = append(result.Columns, col)
	}

	return &result, rows.Err()
}
