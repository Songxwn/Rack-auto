//go:build unix

package agent

import "os"

func isRoot() bool { return os.Geteuid() == 0 }
