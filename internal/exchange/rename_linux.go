package exchange

import "golang.org/x/sys/unix"

func renameExclusive(directory int, from, to string) error {
	return unix.Renameat2(directory, from, directory, to, unix.RENAME_NOREPLACE)
}
