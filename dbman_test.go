package dbman

import (
	"database/sql/driver"
	"reflect"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func Test_DBMan_Query(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}

	rawRows := [][]driver.Value{
		{int64(1), "hello", true},
		{int64(27), "world", true},
		{int64(45), "qux", false},
		{int64(53), nil, nil},
	}
	rows := sqlmock.NewRows([]string{"foo", "bar", "baz"}).
		AddRow(rawRows[0]...).
		AddRow(rawRows[1]...).
		AddRow(rawRows[2]...).
		AddRow(rawRows[3]...)

	mock.ExpectQuery("SELECT foo, bar, baz FROM xyzzy").
		WillReturnRows(rows).
		RowsWillBeClosed()

	dbman := DBMan{
		current: dbMeta{db},
	}

	result, err := dbman.Query("SELECT foo, bar, baz FROM xyzzy")
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Columns) != len(rawRows[0]) {
		t.Fatalf("expected %d columns, but was %d", len(rawRows[0]), len(result.Columns))
	}
	if len(result.Rows) != len(rawRows) {
		t.Fatalf("expected %d rows, but was %d", len(rawRows), len(result.Rows))
	}

	for i, actualRow := range result.Rows {
		expectRow := rawRows[i]

		if len(actualRow) != len(expectRow) {
			t.Errorf("expected %d columns (row #%d), but was %d", len(expectRow), i, len(actualRow))
			continue
		}

		for j, expect := range expectRow {
			actual := actualRow[j]
			if !reflect.DeepEqual(expect, actual) {
				t.Errorf("row #%d, column #%d: expected %v (%[3]T), but got %v (%[4]T)", i, j, expect, actual)
			}
		}
	}
}
