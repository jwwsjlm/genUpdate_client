//go:build !windows

package main

import "fmt"

// GetExeVersion 非 Windows 平台占位实现。
func GetExeVersion(_ string) (string, error) {
	return "", fmt.Errorf("GetExeVersion 仅支持 Windows 平台")
}
