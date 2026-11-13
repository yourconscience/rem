package main

import (
	"fmt"
	"os"
)

func fatal(step string, err error, out string) {
	if out != "" {
		fmt.Print(out)
	}
	fmt.Fprintf(os.Stderr, "rem: %s failed: %v\n", step, err)
	os.Exit(1)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	args := os.Args[2:]
	switch os.Args[1] {
	case "add":
		if helpRequested(args) {
			usage()
			return
		}
		err = cmdAdd(args)
	case "search":
		err = cmdSearch(args)
	case "sync":
		// sync commits and pushes, so a flag it doesn't know must not run it.
		if len(args) > 0 {
			usage()
			if helpRequested(args) {
				return
			}
			os.Exit(2)
		}
		err = cmdSync()
	case "dream":
		err = cmdDream(args)
	case "install":
		err = cmdInstall(args)
	case "hook":
		err = cmdHook(args)
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "rem: unknown command %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fatal(os.Args[1], err, "")
	}
}

// helpRequested reports whether -h or --help appears before a "--" terminator.
func helpRequested(args []string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if a == "-h" || a == "--help" {
			return true
		}
	}
	return false
}

func usage() {
	fmt.Fprint(os.Stderr, `rem - memory workflow for the knowledge vault

usage:
  rem add [-src harness] [--] "<fact>"
                                    capture a note to memory/_inbox, searchable at once
  rem search "<query>"              inbox matches plus semantic search (memsearch, collection ai)
  rem dream [--apply]               v1 consolidation report; --apply collapses exact dups
  rem dream --v2 [flags]            LLM dream over AgentsView transcripts, onto a vault branch
  rem sync                          commit+merge+push via guarded knowledge-sync
  rem install                       materialize hooks/ and lib/ into $REM_HOME
  rem hook session-start            print the session-start hook command
`)
}
