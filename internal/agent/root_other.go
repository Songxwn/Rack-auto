//go:build !unix

package agent

func isRoot() bool { return false }
