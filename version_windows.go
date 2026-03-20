//go:build windows

package main

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// GetExeVersion 获取版本号
func GetExeVersion(filePath string) (string, error) {
	size, err := windows.GetFileVersionInfoSize(filePath, nil)
	if err != nil {
		return "", fmt.Errorf("无法获取版本信息大小: %w", err)
	}

	data := make([]byte, size)
	err = windows.GetFileVersionInfo(filePath, 0, size, unsafe.Pointer(&data[0]))
	if err != nil {
		return "", fmt.Errorf("无法获取版本信息: %w", err)
	}

	var fixedInfo *windows.VS_FIXEDFILEINFO
	var fixedInfoLen uint32
	err = windows.VerQueryValue(unsafe.Pointer(&data[0]), `\\`, unsafe.Pointer(&fixedInfo), &fixedInfoLen)
	if err != nil {
		return "", fmt.Errorf("无法查询版本信息: %w", err)
	}

	major := fixedInfo.FileVersionMS >> 16
	minor := fixedInfo.FileVersionMS & 0xFFFF
	build := fixedInfo.FileVersionLS >> 16
	revision := fixedInfo.FileVersionLS & 0xFFFF
	return fmt.Sprintf("%d.%d.%d.%d", major, minor, build, revision), nil
}
