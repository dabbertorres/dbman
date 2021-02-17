package dbman

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

type DBMan struct {
	current        metaQuerier
	cfg            *Config
	activeQueriers map[string]metaQuerier
	activeTunnels  map[string]*Tunnel
	currentName    string
}

func New(cfg *Config) *DBMan {
	return &DBMan{
		cfg:            cfg,
		current:        nil,
		currentName:    "",
		activeQueriers: make(map[string]metaQuerier),
		activeTunnels:  make(map[string]*Tunnel),
	}
}

func (d *DBMan) Close() error {
	for _, q := range d.activeQueriers {
		q.Close()
	}

	for _, t := range d.activeTunnels {
		t.Close()
	}

	return nil
}

func (d *DBMan) CurrentName() string {
	return d.currentName
}

func (d *DBMan) ListConnections() (names []string, active []bool) {
	names = make([]string, 0, len(d.cfg.Connections))
	active = make([]bool, 0, len(d.cfg.Connections))

	for k := range d.cfg.Connections {
		names = append(names, k)
		_, ok := d.activeQueriers[k]
		active = append(active, ok)
	}
	return names, active
}

func (d *DBMan) SwitchConnection(connName string, prompter ssh.KeyboardInteractiveChallenge) error {
	conn, ok := d.cfg.Connections[connName]
	if !ok {
		return fmt.Errorf("'%s' is not a configured connection", connName)
	}

	querier, ok := d.activeQueriers[connName]
	if ok {
		d.current = querier
		d.currentName = connName
		return nil
	}

	if conn.Tunnel != "" {
		tunnel, ok := d.activeTunnels[conn.Tunnel]
		if !ok {
			tunnelCfg := d.cfg.Tunnels[conn.Tunnel]

			var err error
			tunnel, err = NewTunnel(prompter, &tunnelCfg, conn.Host, conn.Port)
			if err != nil {
				return fmt.Errorf("could not establish tunnel: %w", err)
			}

			d.activeTunnels[conn.Tunnel] = tunnel
		}

		// change connection to point at the local end of the tunnel
		localHost, localPort, _ := net.SplitHostPort(tunnel.localConn.Addr().String())
		conn.Host = localHost
		conn.Port, _ = strconv.Atoi(localPort)
	}

	if conn.Password == "" {
		// is it provided in an environment variable?
		if pgpassword := os.Getenv("PGPASSWORD"); pgpassword != "" {
			conn.Password = pgpassword
		} else {
			answers, err := prompter("", "", []string{"database password: "}, []bool{false})
			if err != nil {
				return err
			}
			conn.Password = answers[0]
		}
	}

	switch conn.Driver {
	case "postgres":
		db, err := postgresOpen(&conn)
		if err != nil {
			return fmt.Errorf("failed to open database connection: %w", err)
		}
		db.SetMaxOpenConns(conn.MaxOpenConns)
		db.SetConnMaxIdleTime(1 * time.Hour)
		querier = dbMeta{db}

	default:
		return errors.New("unsupported database driver")
	}

	ctx := context.Background()
	if conn.ConnectTimeoutSec != 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(conn.ConnectTimeoutSec)*time.Second)
		defer cancel()
	}
	if err := querier.PingContext(ctx); err != nil {
		return fmt.Errorf("failed to connect to database instance: %w", err)
	}

	d.activeQueriers[connName] = querier
	d.current = querier
	d.currentName = connName
	return nil
}

func (d *DBMan) ListTables(schema string) ([]string, error) {
	if d.current == nil {
		return nil, errors.New("an active connection is required")
	}

	if schema != "" {
		return d.current.ListTablesInSchema(schema)
	}
	return d.current.ListTables()
}

func (d *DBMan) ListSchemas() ([]string, error) {
	if d.current == nil {
		return nil, errors.New("an active connection is required")
	}
	return d.current.ListSchemas()
}

func (d *DBMan) DescribeTable(name string) (*TableSchema, error) {
	if d.current == nil {
		return nil, errors.New("an active connection is required")
	}
	return d.current.DescribeTable(name)
}

func (d *DBMan) Stats() sql.DBStats {
	if d.current == nil {
		return sql.DBStats{}
	}
	return d.current.Stats()
}

type QueryResult struct {
	Columns []string
	Rows    [][]interface{}
}

// Query returns a QueryResult with the results of the provided script.
// If no error occurred, and there were no results (e.g, an INSERT/CREATE),
// a nil QueryResult is returned.
func (d *DBMan) Query(script string) (*QueryResult, error) {
	if d.current == nil {
		return nil, errors.New("an active connection is required")
	}

	rows, err := d.current.Query(script)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns, err := rows.ColumnTypes()
	if err != nil {
		return nil, err
	}

	if len(columns) == 0 {
		return nil, nil
	}

	var (
		result = QueryResult{
			Columns: make([]string, len(columns)),
		}

		scanners = make([]interface{}, len(columns))
	)
	for i, col := range columns {
		result.Columns[i] = col.Name()
		switch strings.ToUpper(col.DatabaseTypeName()) {
		case "CHARACTER", "CHAR", "CHARACTER VARYING", "VARCHAR", "NVARCHAR", "TEXT":
			scanners[i] = new(nullString)

		case "BOOL", "BOOLEAN":
			scanners[i] = new(nullBool)

		case "BIGINT", "INT8", "BIGSERIAL", "SERIAL8", "INTERVAL":
			scanners[i] = new(nullInt64)

		case "INTEGER", "INT", "INT4", "SERIAL", "SERIAL4":
			scanners[i] = new(nullInt32)

		case "SMALLINT", "INT2", "SMALLSERIAL", "SERIAL2":
			scanners[i] = new(nullInt16)

		case "DOUBLE", "FLOAT8", "NUMERIC", "DECIMAL":
			scanners[i] = new(nullFloat64)

		case "REAL", "FLOAT4":
			scanners[i] = new(nullFloat32)

		case "TIMESTAMP", "TIMESTAMPTZ", "TIME", "TIMETZ", "DATE":
			scanners[i] = new(nullTime)

		case "UUID":
			scanners[i] = new(uuidVal)

		case "ARRAY":
			scanners[i] = new([]interface{})

		default:
			scanners[i] = reflect.New(col.ScanType()).Interface()
		}
	}

	for rows.Next() {
		if err := rows.Scan(scanners...); err != nil {
			return nil, err
		}

		data := make([]interface{}, len(scanners))
		for i, val := range scanners {
			if val == nil {
				data[i] = nullValue{}
			} else {
				data[i] = reflect.Indirect(reflect.ValueOf(val)).Interface()
			}
		}

		result.Rows = append(result.Rows, data)
	}

	return &result, rows.Err()
}
