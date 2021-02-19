package dbman

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
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

// TODO use SELECT CURRENT_SCHEMA() to decide when and when not to join schema names to table names

func (m dbMeta) ListTables() ([]string, error) {
	rows, err := m.Query(`SELECT format('%s.%s', table_schema, table_name) FROM information_schema.tables
                          WHERE table_schema NOT LIKE 'pg_%'
                          AND table_schema <> 'information_schema'
                          ORDER BY table_schema, table_name`)
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

	return tables, nil
}

func (m dbMeta) ListTablesInSchema(schema string) ([]string, error) {
	rows, err := m.Query(`SELECT table_name FROM information_schema.tables
                          WHERE table_schema = $1
                          ORDER BY table_name`, schema)
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

var (
	ignoreSchemas = []string{
		"pg_*",
		"information_schema",
	}

	// for use in Query() calls
	ignoreSchemasIface = func() []interface{} {
		out := make([]interface{}, len(ignoreSchemas))
		for i, n := range ignoreSchemas {
			out[i] = n
		}
		return out
	}()
)

func (m dbMeta) ListSchemas() ([]string, error) {
	rows, err := m.Query(`SELECT schema_name FROM information_schema.schemata
                          WHERE schema_name NOT LIKE 'pg_%'
                          AND schema_name <> 'information_schema'`)
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
		schemas = append(schemas, name)
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

	rows, err := m.Query(`SELECT column_name, column_default, is_nullable, data_type, udt_schema, udt_name
                          FROM information_schema.columns
                          WHERE table_schema = $1 AND table_name = $2
                          ORDER BY ordinal_position`, schema, table)
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
			defaultVal     sql.NullString
			nullable       yesOrNo
			userTypeSchema sql.NullString
			userType       sql.NullString
		)
		if err := rows.Scan(&col.Name, &defaultVal, &nullable, &col.Type, &userTypeSchema, &userType); err != nil {
			return nil, err
		}

		if col.Type == "USER-DEFINED" {
			col.Type = userTypeSchema.String + "." + userType.String
		}

		if defaultVal.Valid {
			col.Attrs = append(col.Attrs, "DEFAULT "+defaultVal.String)
		}

		if nullable {
			col.Attrs = append(col.Attrs, "NULL")
		} else {
			col.Attrs = append(col.Attrs, "NOT NULL")
		}

		result.Columns = append(result.Columns, col)
	}

	return &result, rows.Err()
}

type yesOrNo bool

func parseYesOrNo(s string) (yesOrNo, error) {
	switch s {
	case "YES", "yes":
		return true, nil
	case "NO", "no":
		return false, nil
	default:
		return false, errors.New("yesOrNo: invalid value")
	}
}

func (v yesOrNo) Value() (driver.Value, error) {
	if v {
		return "YES", nil
	} else {
		return "NO", nil
	}
}

func (v *yesOrNo) Scan(src interface{}) error {
	switch srcVal := src.(type) {
	case string:
		var err error
		*v, err = parseYesOrNo(srcVal)
		return err

	case []byte:
		var err error
		*v, err = parseYesOrNo(string(srcVal))
		return err

	default:
		return errors.New("yesOrNo: incompatible type")
	}
}
