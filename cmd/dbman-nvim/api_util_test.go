package main

import (
	"testing"

	"github.com/neovim/go-client/nvim"
)

func Test_passwordPrompt(t *testing.T) {
	api, _ := initTestEnv(t, nil)
	defer api.Close()

	prompter := passwordPrompt(api)

	go func() {
		if err := api.FeedKeys("42\n", "t", false); err != nil {
			t.Error(err)
		}
	}()

	answers, err := prompter("", "", []string{"what is the answer to the universe?"}, []bool{true})
	if err != nil {
		t.Error("expected not errors to occur:", err)
	} else if len(answers) != 1 {
		t.Error("expected a single answer, got:", len(answers))
	} else {
		if answers[0] != "42" {
			t.Errorf("expected answer to be 42, but got '%s'", answers[0])
		}
	}
}

func Test_openSplitWindow(t *testing.T) {
	windowState := func(api *nvim.Nvim) (windows map[nvim.Window]nvim.Buffer, unattached []nvim.Buffer) {
		t.Helper()

		tab, err := api.CurrentTabpage()
		if err != nil {
			t.Fatal(err)
		}

		winList, err := api.TabpageWindows(tab)
		if err != nil {
			t.Fatal(err)
		}

		bufs, err := api.Buffers()
		if err != nil {
			t.Fatal(err)
		}
		unattached = bufs

		windows = make(map[nvim.Window]nvim.Buffer, len(winList))
		for _, win := range winList {
			buf, err := api.WindowBuffer(win)
			if err != nil {
				t.Fatal(err)
			}
			windows[win] = buf

			for i := 0; i < len(unattached); i++ {
				if unattached[i] == buf {
					unattached = append(unattached[:i], unattached[i+1:]...)
					i--
				}
			}
		}

		return windows, unattached
	}

	t.Run("new_buffer", func(t *testing.T) {
		api, _ := initTestEnv(t, nil)
		defer api.Close()

		buf, win, err := openSplitWindow(api, true, 0)
		if err != nil {
			t.Error(err)
			return
		}

		state, _ := windowState(api)
		if b, ok := state[win]; !ok {
			t.Error("expected new window to be visible")
		} else if b != buf {
			t.Error("expected new buffer to be attached to new window")
		}
	})

	t.Run("existing_buffer", func(t *testing.T) {
		api, _ := initTestEnv(t, nil)
		defer api.Close()

		existingBuf, err := api.CreateBuffer(true, true)
		if err != nil {
			t.Error(err)
			return
		}

		buf, win, err := openSplitWindow(api, true, existingBuf)
		if err != nil {
			t.Error(err)
		}

		if buf != existingBuf {
			t.Errorf("expected window to use existing buffer (%d), not create a new one (%d)", existingBuf, buf)
		}

		state, _ := windowState(api)

		if b, ok := state[win]; !ok {
			t.Error("expected new window to be visible")
		} else if b != buf {
			t.Error("expected existing buffer to be attached to new window")
		}
	})
}

func Test_isBufferVisible(t *testing.T) {
	t.Run("visible", func(t *testing.T) {
		api, _ := initTestEnv(t, nil)
		defer api.Close()

		newBuf, err := api.CreateBuffer(true, true)
		if err != nil {
			t.Error(err)
			return
		}

		if err := api.SetCurrentBuffer(newBuf); err != nil {
			t.Error(err)
			return
		}

		currWin, err := api.CurrentWindow()
		if err != nil {
			t.Error(err)
		}

		visible, win, err := isBufferVisible(api, newBuf)
		if err != nil {
			t.Error(err)
		}

		if !visible {
			t.Error("expected buffer to be visible")
		}
		if win != currWin {
			t.Error("expected buffer to be visible in current window")
		}
	})

	t.Run("not_visible", func(t *testing.T) {
		api, _ := initTestEnv(t, nil)
		defer api.Close()

		newBuf, err := api.CreateBuffer(true, true)
		if err != nil {
			t.Error(err)
			return
		}

		visible, win, err := isBufferVisible(api, newBuf)
		if err != nil {
			t.Error(err)
		}

		if visible {
			t.Error("expected buffer to not be visible")
		}
		if win != 0 {
			t.Error("expected buffer to not be attached to a window")
		}
	})
}
