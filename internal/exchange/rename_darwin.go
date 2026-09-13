package exchange

import "golang.org/x/sys/unix"

func renameExclusive(directory int, from, to string) error {
	return unix.RenameatxNp(directory, from, directory, to, unix.RENAME_EXCL)
}
