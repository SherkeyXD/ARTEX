//go:build windows

package runtime

// Windows does not expose directory fsync through os.File. Files are synced
// before atomic replacement, preserving the existing platform behavior.
func syncSessionDirectory(string) error { return nil }
