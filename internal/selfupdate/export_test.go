package selfupdate

// SetRemoveBackup replaces how Discard removes a backup and returns a func
// that puts the original back.
func SetRemoveBackup(f func(string) error) (restore func()) {
	old := removeBackup
	removeBackup = f
	return func() { removeBackup = old }
}

// SetRestoreRename replaces how Restore moves the backup back and returns a
// func that puts the original back.
func SetRestoreRename(f func(string, string) error) (restore func()) {
	old := restoreRename
	restoreRename = f
	return func() { restoreRename = old }
}
