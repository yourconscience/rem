package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/yourconscience/rem"
)

// remHome is the directory where rem materializes its hooks and library.
func remHome() string {
	return getenvDefault("REM_HOME", filepath.Join(homeDir(), ".local", "share", "rem"))
}

// cmdInstall writes the embedded hooks/ and lib/ into REM_HOME, preserving
// file modes so the hook scripts stay executable.
func cmdInstall(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("rem install takes no arguments")
	}
	home := remHome()
	written := 0
	err := fs.WalkDir(rem.Assets, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := rem.Assets.ReadFile(path)
		if err != nil {
			return err
		}
		dst := filepath.Join(home, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		// go:embed stores files read-only (0444); the hooks are commands and
		// must stay executable.
		if err := os.WriteFile(dst, data, 0o755); err != nil {
			return err
		}
		written++
		return nil
	})
	if err != nil {
		return err
	}
	fmt.Printf("rem: installed %d files to %s\n", written, home)
	return nil
}

// cmdHook prints the command a harness should run for the named event.
func cmdHook(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: rem hook session-start")
	}
	if args[0] != "session-start" {
		return fmt.Errorf("unknown hook %q (only session-start is available)", args[0])
	}
	fmt.Println(filepath.Join(remHome(), "hooks", "session-start.sh"))
	return nil
}
