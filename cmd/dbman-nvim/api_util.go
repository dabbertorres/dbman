package main

import (
	"errors"
	"strconv"

	"github.com/neovim/go-client/nvim"
)

func passwordPrompt(api *nvim.Nvim) func(user, instruction string, questions []string, echos []bool) (answers []string, err error) {
	return func(_, _ string, questions []string, echos []bool) (answers []string, err error) {
		answers = make([]string, len(questions))
		batch := api.NewBatch()

		var outOfMem int
		batch.Call("inputsave", &outOfMem)
		for i, q := range questions {
			if echos[i] {
				batch.Call("input", &answers[i], q)
			} else {
				batch.Call("inputsecret", &answers[i], q)
			}
		}
		batch.Call("inputrestore", &outOfMem)

		if err := batch.Execute(); err != nil {
			return nil, err
		}
		if outOfMem != 0 {
			return nil, errors.New("ran out of memory")
		}
		return answers, nil
	}
}

// openSplitWindow provides a workaround for the lack of an RPC function
// for opening a non-floating window.
// If buf is non-zero, the identified buffer is opened in the new window.
func openSplitWindow(api *nvim.Nvim, vertical bool, buf nvim.Buffer) (nvim.Buffer, nvim.Window, error) {
	var (
		prevBuf nvim.Buffer
		prevWin nvim.Window
	)

	batch := api.NewBatch()
	batch.CurrentBuffer(&prevBuf)
	batch.CurrentWindow(&prevWin)
	if err := batch.Execute(); err != nil {
		return 0, 0, err
	}

	var cmd string
	if buf == 0 {
		if vertical {
			cmd = "vnew"
		} else {
			cmd = "new"
		}
	} else {
		if vertical {
			cmd = "vertical sbuffer "
		} else {
			cmd = "sbuffer "
		}
		cmd += strconv.Itoa(int(buf))
	}

	var win nvim.Window

	batch = api.NewBatch()
	batch.Command(cmd)
	batch.CurrentBuffer(&buf)
	batch.CurrentWindow(&win)

	// scratch
	batch.SetBufferOption(buf, "buftype", "nofile")
	batch.SetBufferOption(buf, "bufhidden", "hide")
	batch.SetBufferOption(buf, "swapfile", false)
	// unlisted
	batch.SetBufferOption(buf, "buflisted", false)

	// reset back to user's active window/buffer
	batch.SetCurrentWindow(prevWin)
	batch.SetCurrentBuffer(prevBuf)

	err := batch.Execute()
	return buf, win, err
}

// isBufferVisible checks if buffer is attached to a Window on the current tabpage.
// If so, it returns true and the Window's ID. Otherwise, it returns false.
func isBufferVisible(api *nvim.Nvim, buffer nvim.Buffer) (bool, nvim.Window, error) {
	tabpage, err := api.CurrentTabpage()
	if err != nil {
		return false, 0, err
	}

	windows, err := api.TabpageWindows(tabpage)
	if err != nil {
		return false, 0, err
	}

	buffers := make([]nvim.Buffer, len(windows))
	batch := api.NewBatch()
	for i, win := range windows {
		batch.WindowBuffer(win, &buffers[i])
	}
	if err := batch.Execute(); err != nil {
		return false, 0, err
	}

	for i, buf := range buffers {
		if buf == buffer {
			return true, windows[i], nil
		}
	}

	return false, 0, nil
}
