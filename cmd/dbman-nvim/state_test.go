package main

import (
	"testing"

	"dabbertorres.dev/dbman"
	"github.com/golang/mock/gomock"
	"github.com/google/go-cmp/cmp"
)

//go:generate mockgen -source=state.go -package=main -destination=mock_dbmanager_test.go dbManager

func Test_pluginState_displaySchemas(t *testing.T) {
	t.Run("creates new window and buffer", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		api, _ := initTestEnv(t, nil)
		defer api.Close()

		mockdb := NewMockdbManager(ctrl)
		mockdb.EXPECT().
			CurrentName().
			Return("mockdb").
			MaxTimes(3)

		state := &pluginState{
			db: mockdb,
		}

		if err := state.displaySchemas(api, false); err != nil {
			t.Error("unexpected error:", err)
		}

		windows, err := api.Windows()
		if err != nil {
			t.Error("unexpected error:", err)
		}

		buffers, err := api.Buffers()
		if err != nil {
			t.Error("unexpected error:", err)
		}

		if len(windows) != 2 {
			t.Error("expected there to be 2 windows, actual:", len(windows))
		}

		if len(buffers) != 2 {
			t.Error("expected there to be 2 buffers, actual:", len(buffers))
		}
	})

	t.Run("uses valid existing window and buffer", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		api, _ := initTestEnv(t, nil)
		defer api.Close()

		mockdb := NewMockdbManager(ctrl)
		mockdb.EXPECT().
			CurrentName().
			Return("mockdb").
			MaxTimes(3)

		state := &pluginState{
			db:         mockdb,
			displayBuf: 1,
			displayWin: 1000,
		}

		if err := state.displaySchemas(api, false); err != nil {
			t.Error("unexpected error:", err)
		}

		windows, err := api.Windows()
		if err != nil {
			t.Error("unexpected error:", err)
		}

		buffers, err := api.Buffers()
		if err != nil {
			t.Error("unexpected error:", err)
		}

		if len(windows) != 1 {
			t.Error("expected there to be 1 window, actual:", len(windows))
		}

		if len(buffers) != 1 {
			t.Error("expected there to be 1 buffer, actual:", len(buffers))
		}
	})

	t.Run("user active window and buffer are active afterwards", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		api, _ := initTestEnv(t, nil)
		defer api.Close()

		mockdb := NewMockdbManager(ctrl)
		mockdb.EXPECT().
			CurrentName().
			Return("mockdb").
			MaxTimes(3)

		state := &pluginState{
			db: mockdb,
		}

		if err := state.displaySchemas(api, false); err != nil {
			t.Error("unexpected error:", err)
		}

		currWin, err := api.CurrentWindow()
		if err != nil {
			t.Error("unexpected error:", err)
		}

		currBuf, err := api.CurrentBuffer()
		if err != nil {
			t.Error("unexpected error:", err)
		}

		if currWin != 1000 {
			t.Error("expected current window to be the original, but was not")
		}

		if currBuf != 1 {
			t.Error("expected current buffer to be the original, but was not")
		}
	})
}

func Test_pluginState_refreshCache(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	api, _ := initTestEnv(t, nil)
	defer api.Close()

	mockdb := NewMockdbManager(ctrl)
	mockdb.EXPECT().
		CurrentName().
		Return("mockdb").
		Times(1)

	mockdb.EXPECT().
		ListSchemas().
		Return([]string{"public", "private"}, error(nil)).
		Times(1)

	mockdb.EXPECT().
		ListTables("public").
		Return([]string{"foo"}, error(nil)).
		Times(1)

	mockdb.EXPECT().
		ListTables("private").
		Return([]string{"qux"}, error(nil)).
		Times(1)

	mockdb.EXPECT().
		DescribeTable("public.foo").
		Return(&dbman.TableSchema{
			Name: "foo",
			Columns: []dbman.ColumnSchema{
				{
					Name:  "foo_id",
					Type:  "uuid",
					Attrs: []string{"NOT NULL"},
				},
				{
					Name:  "state",
					Type:  "integer",
					Attrs: []string{"NULL"},
				},
			},
		}, error(nil)).
		Times(1)

	mockdb.EXPECT().
		DescribeTable("private.qux").
		Return(&dbman.TableSchema{
			Name: "qux",
			Columns: []dbman.ColumnSchema{
				{
					Name:  "qux_id",
					Type:  "uuid",
					Attrs: []string{"NOT NULL"},
				},
				{
					Name:  "valid",
					Type:  "boolean",
					Attrs: []string{"NULL"},
				},
			},
		}, error(nil)).
		Times(1)

	state := &pluginState{
		db:           mockdb,
		displayCache: make(map[string][]schemaState),
	}

	if err := state.refreshCache(); err != nil {
		t.Error("unexpected error:", err)
	}

	cache, ok := state.displayCache["mockdb"]
	if !ok {
		t.Error("expected 'mockdb' cache to be created")
		return
	}

	expect := []schemaState{
		{
			Name: "public",
			Tables: []dbman.TableSchema{
				{
					Name: "foo",
					Columns: []dbman.ColumnSchema{
						{
							Name:  "foo_id",
							Type:  "uuid",
							Attrs: []string{"NOT NULL"},
						},
						{
							Name:  "state",
							Type:  "integer",
							Attrs: []string{"NULL"},
						},
					},
				},
			},
		},
		{
			Name: "private",
			Tables: []dbman.TableSchema{
				{
					Name: "qux",
					Columns: []dbman.ColumnSchema{
						{
							Name:  "qux_id",
							Type:  "uuid",
							Attrs: []string{"NOT NULL"},
						},
						{
							Name:  "valid",
							Type:  "boolean",
							Attrs: []string{"NULL"},
						},
					},
				},
			},
		},
	}
	if diff := cmp.Diff(expect, cache); diff != "" {
		t.Errorf("unexpected cache. diff:\n%s\n", diff)
	}
}

func Test_pluginState_drawSchemas(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	api, _ := initTestEnv(t, nil)
	defer api.Close()

	mockdb := NewMockdbManager(ctrl)
	mockdb.EXPECT().
		CurrentName().
		Return("mockdb").
		Times(1)

	state := &pluginState{
		db:         mockdb,
		displayBuf: 1,
		displayWin: 1000,
		displayCache: map[string][]schemaState{
			"mockdb": {
				{
					Name: "public",
					Tables: []dbman.TableSchema{
						{
							Name: "foo",
							Columns: []dbman.ColumnSchema{
								{
									Name:  "foo_id",
									Type:  "uuid",
									Attrs: []string{"NOT NULL"},
								},
								{
									Name:  "state",
									Type:  "integer",
									Attrs: []string{"NULL"},
								},
							},
						},
					},
				},
				{
					Name: "private",
					Tables: []dbman.TableSchema{
						{
							Name: "qux",
							Columns: []dbman.ColumnSchema{
								{
									Name:  "qux_id",
									Type:  "uuid",
									Attrs: []string{"NOT NULL"},
								},
								{
									Name:  "valid",
									Type:  "boolean",
									Attrs: []string{"NOT NULL"},
								},
							},
						},
					},
				},
			},
		},
	}

	batch := api.NewBatch()
	_ = state.drawSchemas(batch, 2)
	if err := batch.Execute(); err != nil {
		t.Error("unexpected error:", err)
	}

	actualLines, err := api.BufferLines(state.displayBuf, 0, -1, false)
	if err != nil {
		t.Error("unexpected error:", err)
	}

	expectLines := []string{
		"public",
		"  foo",
		"    foo_id | uuid    | NOT NULL",
		"    state  | integer | NULL",
		"private",
		"  qux",
		"    qux_id | uuid    | NOT NULL",
		"    valid  | boolean | NOT NULL",
		"",
	}

	if len(actualLines) != len(expectLines) {
		t.Errorf("expected %d lines, but was %d lines", len(expectLines), len(actualLines))
	}

	shortestLen := len(actualLines)
	if len(expectLines) < shortestLen {
		shortestLen = len(expectLines)
	}

	for i := 0; i < shortestLen; i++ {
		expect := expectLines[i]
		line := string(actualLines[i])

		if line != expect {
			t.Errorf("line #%d:\n\texpected: '%s'\n\t  actual: '%s'\n", i, expect, line)
		}
	}
}
