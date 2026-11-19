//go:build !linux

package store

func diskFree(string) (int64, error) {
	return 1 << 62, nil
}
